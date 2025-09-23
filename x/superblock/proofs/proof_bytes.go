package proofs

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

// ProofBytes handles flexible JSON representations of proof payloads.
//
// Accepts either 0x-prefixed hex strings, base64 strings, or arrays of byte
// values. MarshalJSON always emits a 0x-prefixed hex string for consistency.
type ProofBytes []byte

// UnmarshalJSON implements json.Unmarshaler, allowing multiple encodings.
func (p *ProofBytes) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*p = nil
		return nil
	}
	if data[0] == '[' {
		var ints []int
		if err := json.Unmarshal(data, &ints); err != nil {
			return fmt.Errorf("proof array must contain integers: %w", err)
		}
		buf := make([]byte, len(ints))
		for i, v := range ints {
			if v < 0 || v > 255 {
				return fmt.Errorf("proof byte out of range: %d", v)
			}
			buf[i] = byte(v)
		}
		*p = ProofBytes(buf)
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("proof string invalid: %w", err)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*p = nil
			return nil
		}
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			decoded, err := hexutil.Decode(s)
			if err != nil {
				return fmt.Errorf("proof hex decode failed: %w", err)
			}
			*p = ProofBytes(decoded)
			return nil
		}
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return fmt.Errorf("proof base64 decode failed: %w", err)
		}
		*p = ProofBytes(decoded)
		return nil
	}
	return fmt.Errorf("unsupported proof encoding")
}

// MarshalJSON emits a 0x-prefixed hex string representation.
func (p ProofBytes) MarshalJSON() ([]byte, error) {
	if len(p) == 0 {
		return []byte("null"), nil
	}
	return json.Marshal(hexutil.Encode(p))
}

// Clone returns a defensive copy of the underlying slice.
func (p ProofBytes) Clone() []byte {
	if len(p) == 0 {
		return nil
	}
	buf := make([]byte, len(p))
	copy(buf, p)
	return buf
}

// Bytes returns the underlying slice without copying.
func (p ProofBytes) Bytes() []byte {
	return p
}

// PublicValueBytes handles public values for the superblock-prover.
// Unlike ProofBytes, this marshals as an array of integers, not hex string.
type PublicValueBytes []byte

// UnmarshalJSON implements json.Unmarshaler, allowing multiple encodings.
func (p *PublicValueBytes) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*p = nil
		return nil
	}
	if data[0] == '[' {
		var ints []int
		if err := json.Unmarshal(data, &ints); err != nil {
			return fmt.Errorf("public values array must contain integers: %w", err)
		}
		buf := make([]byte, len(ints))
		for i, v := range ints {
			if v < 0 || v > 255 {
				return fmt.Errorf("public value byte out of range: %d", v)
			}
			buf[i] = byte(v)
		}
		*p = PublicValueBytes(buf)
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("public values string invalid: %w", err)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*p = nil
			return nil
		}
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			decoded, err := hexutil.Decode(s)
			if err != nil {
				return fmt.Errorf("public values hex decode failed: %w", err)
			}
			*p = PublicValueBytes(decoded)
			return nil
		}
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return fmt.Errorf("public values base64 decode failed: %w", err)
		}
		*p = PublicValueBytes(decoded)
		return nil
	}
	return fmt.Errorf("unsupported public values encoding")
}

// MarshalJSON emits an array of integers for the superblock-prover.
func (p PublicValueBytes) MarshalJSON() ([]byte, error) {
	if len(p) == 0 {
		return []byte("[]"), nil
	}
	ints := make([]int, len(p))
	for i, b := range p {
		ints[i] = int(b)
	}
	return json.Marshal(ints)
}

// Bytes returns the underlying slice without copying.
func (p PublicValueBytes) Bytes() []byte {
	return p
}
