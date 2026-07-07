package delta

import (
	"bytes"
	"crypto/rand"
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
	rand.Read(base)

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
	rand.Read(data)

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
	data := make([]byte, 1<<20) // 1MB
	rand.Read(data)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SignBytes(data, DefaultBlockSize)
	}
}

func BenchmarkDiffApply(b *testing.B) {
	base := make([]byte, 1<<20)
	modified := make([]byte, 1<<20)
	rand.Read(base)
	copy(modified, base)
	modified[len(modified)/2] = ^modified[len(modified)/2]

	sig := SignBytes(base, DefaultBlockSize)
	b.SetBytes(int64(len(base)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ops, _ := DiffBytes(sig, modified)
		Apply(base, sig.BlockSize, ops)
	}
}
