package delta

import (
	"bytes"
	"crypto/rand"
	mrand "math/rand"
	"testing"
)

func TestSignAndApplyIdentical(t *testing.T) {
	data := []byte("hello world this is a test file that spans multiple blocks. " +
		"it needs to be long enough to generate at least two blocks. " +
		"let me add some more content here to make sure we reach the block size threshold. " +
		"this should be enough data now to work properly.")

	sig := SignBytes(data, 64)
	if len(sig.Blocks) == 0 {
		t.Fatal("expected at least one block")
	}

	ops, err := DiffBytes(sig, data)
	if err != nil {
		t.Fatal(err)
	}

	reconstructed, err := Apply(data, sig.BlockSize, ops)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, reconstructed) {
		t.Fatalf("data mismatch:\n  original: %q\n  reconstructed: %q", data, reconstructed)
	}

	// For identical data, most ops should be Copy (last partial block may be Data)
	copyCount := 0
	for _, op := range ops {
		if op.Copy {
			copyCount++
		}
	}
	if copyCount == 0 {
		t.Errorf("expected at least one Copy op for identical data")
	}
}

func TestSignAndApplyModified(t *testing.T) {
	base := []byte("AAAA" + "BBBB" + "CCCC" + "DDDD" + "EEEE")
	modified := []byte("AAAA" + "BBBB" + "XXXX" + "DDDD" + "EEEE")

	sig := SignBytes(base, 4)
	ops, err := DiffBytes(sig, modified)
	if err != nil {
		t.Fatal(err)
	}

	reconstructed, err := Apply(base, sig.BlockSize, ops)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(modified, reconstructed) {
		t.Fatalf("expected %q, got %q", modified, reconstructed)
	}
}

func TestSignAndApplyInsertion(t *testing.T) {
	base := []byte("AAAABBBBCCCCDDDDEEEE")
	modified := []byte("AAAA" + "BBBB" + "INSERTED" + "CCCCDDDDEEEE")

	sig := SignBytes(base, 4)
	ops, err := DiffBytes(sig, modified)
	if err != nil {
		t.Fatal(err)
	}

	reconstructed, err := Apply(base, sig.BlockSize, ops)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(modified, reconstructed) {
		t.Fatalf("expected %q, got %q", modified, reconstructed)
	}
}

func TestSignAndApplyDeletion(t *testing.T) {
	base := []byte("AAAABBBBCCCCDDDDEEEE")
	modified := []byte("AAAABBBBCCCCEEEE")

	sig := SignBytes(base, 4)
	ops, err := DiffBytes(sig, modified)
	if err != nil {
		t.Fatal(err)
	}

	reconstructed, err := Apply(base, sig.BlockSize, ops)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(modified, reconstructed) {
		t.Fatalf("expected %q, got %q", modified, reconstructed)
	}
}

func TestEmptyFile(t *testing.T) {
	sig := SignBytes([]byte{}, 64)
	if len(sig.Blocks) != 0 {
		t.Fatal("expected empty signature")
	}

	ops, err := DiffBytes(sig, []byte{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || len(ops[0].Data) != 0 {
		t.Fatal("expected single empty data op for empty diff")
	}

	result, err := Apply([]byte{}, 64, ops)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatal("expected empty result")
	}
}

func TestFileSmallerThanBlock(t *testing.T) {
	data := []byte("small")
	sig := SignBytes(data, 64)
	if len(sig.Blocks) != 1 {
		t.Fatal("expected 1 block for small file")
	}

	ops, err := DiffBytes(sig, data)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Apply(data, sig.BlockSize, ops)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, result) {
		t.Fatalf("expected %q, got %q", data, result)
	}
}

func TestLargeFileRandomChanges(t *testing.T) {
	base := make([]byte, 65536)
	_, _ = rand.Read(base)

	modified := make([]byte, len(base))
	copy(modified, base)
	// Change some bytes
	modified[1000] = ^modified[1000]
	modified[32000] = ^modified[32000]
	modified[64000] = ^modified[64000]

	sig := SignBytes(base, 4096)
	ops, err := DiffBytes(sig, modified)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Apply(base, sig.BlockSize, ops)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(modified, result) {
		t.Fatal("reconstructed data differs from modified")
	}
}

func TestMarshalUnmarshalOps(t *testing.T) {
	ops := []Op{
		{Copy: true, Index: 42},
		{Data: []byte("literal data here")},
		{Copy: true, Index: 7},
		{Data: []byte("more literal")},
	}

	data := MarshalOps(ops)
	decoded, err := UnmarshalOps(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded) != len(ops) {
		t.Fatalf("expected %d ops, got %d", len(ops), len(decoded))
	}

	for i, op := range ops {
		if op.Copy != decoded[i].Copy {
			t.Errorf("op %d: Copy mismatch", i)
		}
		if op.Copy && op.Index != decoded[i].Index {
			t.Errorf("op %d: Index %d != %d", i, op.Index, decoded[i].Index)
		}
		if !op.Copy && !bytes.Equal(op.Data, decoded[i].Data) {
			t.Errorf("op %d: Data mismatch", i)
		}
	}
}

func TestMarshalUnmarshalSignature(t *testing.T) {
	data := []byte("test data for signature marshal round trip")
	sig := SignBytes(data, 4)

	encoded := MarshalSignature(sig)
	decoded, err := UnmarshalSignature(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if sig.BlockSize != decoded.BlockSize {
		t.Errorf("BlockSize: %d != %d", sig.BlockSize, decoded.BlockSize)
	}
	if len(sig.Blocks) != len(decoded.Blocks) {
		t.Fatalf("Blocks count: %d != %d", len(sig.Blocks), len(decoded.Blocks))
	}

	for i := range sig.Blocks {
		if sig.Blocks[i].Weak != decoded.Blocks[i].Weak {
			t.Errorf("block %d: Weak hash mismatch", i)
		}
		if sig.Blocks[i].Strong != decoded.Blocks[i].Strong {
			t.Errorf("block %d: Strong hash mismatch", i)
		}
	}
}

func TestSignNonDefaultBlockSize(t *testing.T) {
	data := make([]byte, 100)
	_, _ = rand.Read(data)

	sig := SignBytes(data, 32)
	if sig.BlockSize != 32 {
		t.Errorf("BlockSize = %d, want 32", sig.BlockSize)
	}

	// 100 bytes / 32 = 3.125, so we should have 4 blocks (last one partial)
	if len(sig.Blocks) != 4 {
		t.Errorf("expected 4 blocks for 100 bytes at 32-block, got %d", len(sig.Blocks))
	}
}

func TestApplyBlockOutOfRange(t *testing.T) {
	ops := []Op{{Copy: true, Index: 999}}
	_, err := Apply([]byte("small"), 4, ops)
	if err == nil {
		t.Fatal("expected error for out-of-range block")
	}
}

func BenchmarkSign(b *testing.B) {
	data := make([]byte, 1<<20)
	_, _ = rand.Read(data)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SignBytes(data, DefaultBlockSize)
	}
}

func BenchmarkDiffApply(b *testing.B) {
	base := make([]byte, 1<<20)
	modified := make([]byte, 1<<20)
	_, _ = rand.Read(base)
	copy(modified, base)
	modified[len(modified)/2] = ^modified[len(modified)/2]

	sig := SignBytes(base, DefaultBlockSize)
	b.SetBytes(int64(len(base)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ops, _ := DiffBytes(sig, modified)
		_, _ = Apply(base, sig.BlockSize, ops)
	}
}

// The rolling checksum must stay identical to a full recomputation of every
// window. The old uint32 update wrapped on negative intermediates and
// drifted permanently after the first one, silently losing all later
// matches - these tests are the regression guard for that.
func TestRollMatchesFullRecompute(t *testing.T) {
	rng := mrand.New(mrand.NewSource(1))
	data := make([]byte, 3*DefaultBlockSize+100)
	rng.Read(data)

	a, b := rollInit(data[:DefaultBlockSize])
	for pos := 0; pos+DefaultBlockSize < len(data); pos++ {
		wantA, wantB := rollInit(data[pos : pos+DefaultBlockSize])
		if a != wantA || b != wantB {
			t.Fatalf("pos %d: rolling (%d,%d) != full (%d,%d)", pos, a, b, wantA, wantB)
		}
		a, b = rollUpdate(a, b, DefaultBlockSize, uint32(data[pos]), uint32(data[pos+DefaultBlockSize]))
	}
}

func TestRollMatchesFullRecomputeSmallBlock(t *testing.T) {
	rng := mrand.New(mrand.NewSource(2))
	data := make([]byte, 1000)
	rng.Read(data)

	const bs = 4
	a, b := rollInit(data[:bs])
	for pos := 0; pos+bs < len(data); pos++ {
		wantA, wantB := rollInit(data[pos : pos+bs])
		if a != wantA || b != wantB {
			t.Fatalf("pos %d: rolling (%d,%d) != full (%d,%d)", pos, a, b, wantA, wantB)
		}
		a, b = rollUpdate(a, b, bs, uint32(data[pos]), uint32(data[pos+bs]))
	}
}

// A file shifted by a few bytes of inserted content must still match all of
// its old blocks: the rolling checksum exists precisely to find block
// boundaries that no longer line up. Fixed-size hashing would miss every
// match here.
func TestDiffShiftedContent(t *testing.T) {
	rng := mrand.New(mrand.NewSource(42))
	base := make([]byte, 4*DefaultBlockSize)
	rng.Read(base)
	shifted := append([]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE}, base...)

	sig, err := Sign(bytes.NewReader(base), DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := Diff(sig, bytes.NewReader(shifted))
	if err != nil {
		t.Fatal(err)
	}

	copies := 0
	for _, op := range ops {
		if op.Copy {
			copies++
		}
	}
	if copies != 4 {
		t.Errorf("shifted content matched %d blocks, want 4", copies)
	}

	reconstructed, err := Apply(base, sig.BlockSize, ops)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reconstructed, shifted) {
		t.Fatal("Apply(ops) of shifted content mismatch")
	}
}
