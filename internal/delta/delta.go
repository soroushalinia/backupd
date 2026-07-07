package delta

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"io"
)

const DefaultBlockSize = 8192

type BlockChecksum struct {
	Weak   uint32
	Strong [sha256.Size]byte
}

type Signature struct {
	BlockSize int
	Blocks    []BlockChecksum
}

type Op struct {
	Copy  bool
	Index int
	Data  []byte
}

func Sign(r io.Reader, blockSize int) (*Signature, error) {
	if blockSize <= 0 {
		blockSize = DefaultBlockSize
	}
	sig := &Signature{BlockSize: blockSize}
	buf := make([]byte, blockSize)
	for {
		n, err := io.ReadFull(r, buf)
		if n == 0 && err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		chunk := buf[:n]
		var bc BlockChecksum
		bc.Weak = adler32(chunk)
		bc.Strong = sha256.Sum256(chunk)
		sig.Blocks = append(sig.Blocks, bc)
		if err != nil && err != io.ErrUnexpectedEOF {
			break
		}
	}
	return sig, nil
}

func SignBytes(data []byte, blockSize int) *Signature {
	r := bytes.NewReader(data)
	sig, _ := Sign(r, blockSize)
	return sig
}

func Diff(sig *Signature, newContent io.Reader) ([]Op, error) {
	weakMap := buildWeakMap(sig)
	blockSize := sig.BlockSize

	data, err := io.ReadAll(newContent)
	if err != nil {
		return nil, err
	}

	if len(data) < blockSize {
		return []Op{{Data: append([]byte(nil), data...)}}, nil
	}

	var ops []Op
	var literalBuf bytes.Buffer

	a, b := rollInit(data[:blockSize])
	weak := (b << 16) | a

	pos := 0
	for pos < len(data) {
		remaining := len(data) - pos
		if remaining >= blockSize {
			if indices, ok := weakMap[weak]; ok {
				strong := sha256.Sum256(data[pos : pos+blockSize])
				if match := findMatch(sig, indices, strong); match >= 0 {
					flushLiterals(&ops, &literalBuf)
					ops = append(ops, Op{Copy: true, Index: match})
					pos += blockSize
					if pos+blockSize <= len(data) {
						a, b = rollInit(data[pos : pos+blockSize])
						weak = (b << 16) | a
					}
					continue
				}
			}
		}

		literalBuf.WriteByte(data[pos])

		if pos+blockSize < len(data) {
			oldByte := uint32(data[pos])
			newByte := uint32(data[pos+blockSize])
			a = (a - oldByte + newByte) % 65521
			b = (b - uint32(blockSize)*oldByte + a) % 65521
			weak = (b << 16) | a
		}
		pos++
	}

	flushLiterals(&ops, &literalBuf)
	return ops, nil
}

func DiffBytes(sig *Signature, newData []byte) ([]Op, error) {
	return Diff(sig, bytes.NewReader(newData))
}

func Apply(baseData []byte, blockSize int, ops []Op) ([]byte, error) {
	if blockSize <= 0 {
		blockSize = DefaultBlockSize
	}
	var out bytes.Buffer
	for _, op := range ops {
		if op.Copy {
			start := op.Index * blockSize
			if start >= len(baseData) {
				return nil, errBlockOutOfRange
			}
			end := start + blockSize
			if end > len(baseData) {
				end = len(baseData)
			}
			out.Write(baseData[start:end])
		} else {
			out.Write(op.Data)
		}
	}
	return out.Bytes(), nil
}

func buildWeakMap(sig *Signature) map[uint32][]int {
	m := make(map[uint32][]int)
	for i, b := range sig.Blocks {
		m[b.Weak] = append(m[b.Weak], i)
	}
	return m
}

func findMatch(sig *Signature, indices []int, strong [sha256.Size]byte) int {
	for _, idx := range indices {
		if strong == sig.Blocks[idx].Strong {
			return idx
		}
	}
	return -1
}

func flushLiterals(ops *[]Op, buf *bytes.Buffer) {
	if buf.Len() > 0 {
		data := make([]byte, buf.Len())
		copy(data, buf.Bytes())
		*ops = append(*ops, Op{Data: data})
		buf.Reset()
	}
}

func rollInit(data []byte) (uint32, uint32) {
	var a, b uint32 = 1, 0
	for _, c := range data {
		a = (a + uint32(c)) % 65521
		b = (b + a) % 65521
	}
	return a, b
}

func adler32(data []byte) uint32 {
	a, b := rollInit(data)
	return (b << 16) | a
}

var errBlockOutOfRange = errBlockOutOfRangeType{}

type errBlockOutOfRangeType struct{}

func (errBlockOutOfRangeType) Error() string {
	return "block index out of range"
}

func MarshalOps(ops []Op) []byte {
	var buf bytes.Buffer
	for _, op := range ops {
		if op.Copy {
			buf.WriteByte(1)
			binary.Write(&buf, binary.BigEndian, uint32(op.Index))
		} else {
			buf.WriteByte(0)
			binary.Write(&buf, binary.BigEndian, uint32(len(op.Data)))
			buf.Write(op.Data)
		}
	}
	return buf.Bytes()
}

func UnmarshalOps(data []byte) ([]Op, error) {
	r := bytes.NewReader(data)
	var ops []Op
	for r.Len() > 0 {
		typ, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if typ == 1 {
			var idx uint32
			if err := binary.Read(r, binary.BigEndian, &idx); err != nil {
				return nil, err
			}
			ops = append(ops, Op{Copy: true, Index: int(idx)})
		} else {
			var n uint32
			if err := binary.Read(r, binary.BigEndian, &n); err != nil {
				return nil, err
			}
			chunk := make([]byte, n)
			if _, err := io.ReadFull(r, chunk); err != nil {
				return nil, err
			}
			ops = append(ops, Op{Data: chunk})
		}
	}
	return ops, nil
}

func MarshalSignature(sig *Signature) []byte {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(sig.BlockSize))
	binary.Write(&buf, binary.BigEndian, uint32(len(sig.Blocks)))
	for _, b := range sig.Blocks {
		binary.Write(&buf, binary.BigEndian, b.Weak)
		buf.Write(b.Strong[:])
	}
	return buf.Bytes()
}

func UnmarshalSignature(data []byte) (*Signature, error) {
	r := bytes.NewReader(data)
	var blockSize uint32
	if err := binary.Read(r, binary.BigEndian, &blockSize); err != nil {
		return nil, err
	}
	var n uint32
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return nil, err
	}
	sig := &Signature{BlockSize: int(blockSize), Blocks: make([]BlockChecksum, n)}
	for i := range sig.Blocks {
		if err := binary.Read(r, binary.BigEndian, &sig.Blocks[i].Weak); err != nil {
			return nil, err
		}
		if _, err := io.ReadFull(r, sig.Blocks[i].Strong[:]); err != nil {
			return nil, err
		}
	}
	return sig, nil
}
