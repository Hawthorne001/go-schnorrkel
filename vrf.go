package schnorrkel

import (
	"errors"

	"github.com/gtank/merlin"
	r255 "github.com/gtank/ristretto255"
)

// MAX_VRF_BYTES is the maximum bytes that can be extracted from the VRF via MakeBytes
const MAX_VRF_BYTES = 64

var kusamaVRF = true

const VRFLabel = "VRF"

type VrfInOut struct {
	input  *r255.Element
	output *r255.Element
}

type VrfOutput struct {
	output *r255.Element
}

type VrfProof struct {
	c *r255.Scalar
	s *r255.Scalar
}

// SetKusama sets the VRF kusama option. Defaults to true.
func SetKusamaVRF(k bool) {
	kusamaVRF = k
}

// Output returns a VrfOutput from a VrfInOut
func (io *VrfInOut) Output() *VrfOutput {
	return &VrfOutput{
		output: io.output,
	}
}

// EncodeOutput returns the 64-byte encoding of the input and output concatenated
func (io *VrfInOut) Encode() []byte {
	outbytes := [32]byte{}
	copy(outbytes[:], io.output.Encode([]byte{}))
	inbytes := [32]byte{}
	copy(inbytes[:], io.input.Encode([]byte{}))
	return append(inbytes[:], outbytes[:]...)
}

// MakeBytes returns raw bytes output from the VRF
// It returns a byte slice of the given size
// https://github.com/w3f/schnorrkel/blob/master/src/vrf.rs#L343
func (io *VrfInOut) MakeBytes(size int, context []byte) ([]byte, error) {
	if size <= 0 || size > MAX_VRF_BYTES {
		return nil, errors.New("invalid size parameter")
	}

	t := merlin.NewTranscript("VRFResult")
	t.AppendMessage([]byte(""), context)
	io.commit(t)
	return t.ExtractBytes([]byte(""), size), nil
}

func (io *VrfInOut) commit(t *merlin.Transcript) {
	t.AppendMessage([]byte("vrf-in"), io.input.Encode([]byte{}))
	t.AppendMessage([]byte("vrf-out"), io.output.Encode([]byte{}))
}

// NewOutput creates a new VRF output from a 64-byte element
func NewOutput(in [32]byte) (*VrfOutput, error) {
	output := r255.NewElement()
	err := output.Decode(in[:])
	if err != nil {
		return nil, err
	}

	return &VrfOutput{
		output: output,
	}, nil
}

// AttachInput returns a VrfInOut pair from an output
// https://github.com/w3f/schnorrkel/blob/master/src/vrf.rs#L249
func (out *VrfOutput) AttachInput(pub *PublicKey, t *merlin.Transcript) (*VrfInOut, error) {
	if pub == nil {
		return nil, errors.New("public key provided is nil")
	}

	if t == nil {
		return nil, errors.New("transcript provided is nil")
	}

	input := pub.vrfHash(t)
	return &VrfInOut{
		input:  input,
		output: out.output,
	}, nil
}

// Encode returns the 32-byte encoding of the output
func (out *VrfOutput) Encode() [32]byte {
	outbytes := [32]byte{}
	copy(outbytes[:], out.output.Encode([]byte{}))
	return outbytes
}

// Decode sets the VrfOutput to the decoded input
func (out *VrfOutput) Decode(in [32]byte) error {
	output := r255.NewElement()
	err := output.Decode(in[:])
	if err != nil {
		return err
	}
	out.output = output
	return nil
}

// Encode returns a 64-byte encoded VrfProof
func (p *VrfProof) Encode() [64]byte {
	cbytes := [32]byte{}
	copy(cbytes[:], p.c.Encode([]byte{}))
	sbytes := [32]byte{}
	copy(sbytes[:], p.s.Encode([]byte{}))
	enc := [64]byte{}
	copy(enc[:32], cbytes[:])
	copy(enc[32:], sbytes[:])
	return enc
}

// Decode sets the VrfProof to the decoded input
func (p *VrfProof) Decode(in [64]byte) error {
	c := r255.NewScalar()
	err := c.Decode(in[:32])
	if err != nil {
		return err
	}
	p.c = c

	s := r255.NewScalar()
	err = s.Decode(in[32:])
	if err != nil {
		return err
	}
	p.s = s

	return nil
}

// VrfSign returns a vrf output and proof given a secret key and transcript.
func (kp *Keypair) VrfSign(t *merlin.Transcript) (*VrfInOut, *VrfProof, error) {
	if kp.secretKey == nil {
		return nil, nil, errors.New("secretKey is nil")
	}
	return kp.secretKey.VrfSign(t)
}

// VrfVerify verifies that the proof and output created are valid given the public key and transcript.
func (kp *Keypair) VrfVerify(t *merlin.Transcript, out *VrfOutput, proof *VrfProof) (bool, error) {
	if kp.publicKey == nil {
		return false, errors.New("publicKey is nil")
	}
	return kp.publicKey.VrfVerify(t, out, proof)
}

// VrfSign returns a vrf output and proof given a secret key and transcript.
func (secretKey *SecretKey) VrfSign(t *merlin.Transcript) (*VrfInOut, *VrfProof, error) {
	if t == nil {
		return nil, nil, errors.New("transcript provided is nil")
	}

	p, err := secretKey.vrfCreateHash(t)
	if err != nil {
		return nil, nil, err
	}

	extra := merlin.NewTranscript(VRFLabel)
	proof, err := secretKey.dleqProve(extra, p)
	if err != nil {
		return nil, nil, err
	}
	return p, proof, nil
}

// dleqProve creates a VRF proof for the transcript and input with this secret key.
// see: https://github.com/w3f/schnorrkel/blob/798ab3e0813aa478b520c5cf6dc6e02fd4e07f0a/src/vrf.rs#L604
func (secretKey *SecretKey) dleqProve(t *merlin.Transcript, p *VrfInOut) (*VrfProof, error) {
	pub, err := secretKey.Public()
	if err != nil {
		return nil, err
	}
	pubenc := pub.Encode()

	t.AppendMessage([]byte("proto-name"), []byte("DLEQProof"))
	t.AppendMessage([]byte("vrf:h"), p.input.Encode([]byte{}))
	if !kusamaVRF {
		t.AppendMessage([]byte("vrf:pk"), pubenc[:])
	}

	// create random element R = g^r
	// TODO: update toe use witness scalar
	// https://github.com/w3f/schnorrkel/blob/master/src/vrf.rs#L620
	r, err := NewRandomScalar()
	if err != nil {
		return nil, err
	}
	R := r255.NewElement()
	R.ScalarBaseMult(r)
	t.AppendMessage([]byte("vrf:R=g^r"), R.Encode([]byte{}))

	// create hr := HashToElement(input)
	hr := r255.NewElement().ScalarMult(r, p.input).Encode([]byte{})
	t.AppendMessage([]byte("vrf:h^r"), hr)

	if kusamaVRF {
		t.AppendMessage([]byte("vrf:pk"), pubenc[:])
	}
	t.AppendMessage([]byte("vrf:h^sk"), p.output.Encode([]byte{}))

	c := challengeScalar(t, []byte("prove"))
	s := r255.NewScalar()
	sc, err := ScalarFromBytes(secretKey.key)
	if err != nil {
		return nil, err
	}
	s.Subtract(r, r255.NewScalar().Multiply(c, sc))

	return &VrfProof{
		c: c,
		s: s,
	}, nil
}

// vrfCreateHash creates a VRF input/output pair on the given transcript.
func (secretKey *SecretKey) vrfCreateHash(t *merlin.Transcript) (*VrfInOut, error) {
	pub, err := secretKey.Public()
	if err != nil {
		return nil, err
	}
	input := pub.vrfHash(t)

	output := r255.NewElement()
	sc := r255.NewScalar()
	err = sc.Decode(secretKey.key[:])
	if err != nil {
		return nil, err
	}
	output.ScalarMult(sc, input)

	return &VrfInOut{
		input:  input,
		output: output,
	}, nil
}

// VrfVerify verifies that the proof and output created are valid given the public key and transcript.
func (publicKey *PublicKey) VrfVerify(t *merlin.Transcript, out *VrfOutput, proof *VrfProof) (bool, error) {
	if t == nil {
		return false, errors.New("transcript provided is nil")
	}

	if out == nil {
		return false, errors.New("output provided is nil")
	}

	if proof == nil {
		return false, errors.New("proof provided is nil")
	}

	if publicKey.key.Equal(publicKeyAtInfinity) == 1 {
		return false, ErrPublicKeyAtInfinity
	}

	inout, err := out.AttachInput(publicKey, t)
	if err != nil {
		return false, err
	}

	t0 := merlin.NewTranscript(VRFLabel)
	return publicKey.dleqVerify(t0, inout, proof)
}

// dleqVerify verifies the corresponding dleq proof.
func (publicKey *PublicKey) dleqVerify(t *merlin.Transcript, p *VrfInOut, proof *VrfProof) (bool, error) {
	t.AppendMessage([]byte("proto-name"), []byte("DLEQProof"))
	t.AppendMessage([]byte("vrf:h"), p.input.Encode([]byte{}))
	if !kusamaVRF {
		t.AppendMessage([]byte("vrf:pk"), publicKey.key.Encode([]byte{}))
	}

	// R = proof.c*pk + proof.s*g
	R := r255.NewElement()
	R.VarTimeDoubleScalarBaseMult(proof.c, publicKey.key, proof.s)
	t.AppendMessage([]byte("vrf:R=g^r"), R.Encode([]byte{}))

	// hr = proof.c * p.output + proof.s * p.input
	hr := r255.NewElement().VarTimeMultiScalarMult([]*r255.Scalar{proof.c, proof.s}, []*r255.Element{p.output, p.input})
	t.AppendMessage([]byte("vrf:h^r"), hr.Encode([]byte{}))
	if kusamaVRF {
		t.AppendMessage([]byte("vrf:pk"), publicKey.key.Encode([]byte{}))
	}
	t.AppendMessage([]byte("vrf:h^sk"), p.output.Encode([]byte{}))

	cexpected := challengeScalar(t, []byte("prove"))
	if cexpected.Equal(proof.c) == 1 {
		return true, nil
	}

	return false, nil
}

// vrfHash hashes the transcript to a point.
func (publicKey *PublicKey) vrfHash(t *merlin.Transcript) *r255.Element {
	mt := TranscriptWithMalleabilityAddressed(t, publicKey)
	hash := mt.ExtractBytes([]byte("VRFHash"), 64)
	point := r255.NewElement()
	point.FromUniformBytes(hash)
	return point
}

// TranscriptWithMalleabilityAddressed returns the input transcript with the public key commited to it,
// addressing VRF output malleability.
func TranscriptWithMalleabilityAddressed(t *merlin.Transcript, pk *PublicKey) *merlin.Transcript {
	enc := pk.Encode()
	t.AppendMessage([]byte("vrf-nm-pk"), enc[:])
	return t
}
