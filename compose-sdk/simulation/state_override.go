package simulation

import (
	"math/big"

	"github.com/compose-network/compose-sdk/mailbox"
	"github.com/compose-network/specs/compose"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Mailbox contract storage slots
const (
	inboxMappingSlot       = 4 // mapping(bytes32 => bytes) inbox
	createdKeysMappingSlot = 6 // mapping(bytes32 => bool) createdKeys
)

// BuildMailboxStateOverrides builds state overrides for mailbox dependencies.
func BuildMailboxStateOverrides(
	chainID compose.ChainID,
	mailboxAddr common.Address,
	deps []mailbox.CrossRollupDependency,
) map[string]any {
	if mailboxAddr == (common.Address{}) {
		return nil
	}

	stateDiff := make(map[string]string)
	for _, dep := range deps {
		if !dep.IsInboxRead || dep.DestChainID != chainID {
			continue
		}

		key, ok := mailboxKey(chainID, dep)
		if !ok {
			continue
		}

		inboxSlot := mappingSlot(key, inboxMappingSlot)
		createdSlot := mappingSlot(key, createdKeysMappingSlot)

		applyBytesToStateDiff(stateDiff, inboxSlot, dep.Data)
		stateDiff[createdSlot.Hex()] = common.BigToHash(big.NewInt(1)).Hex()
	}

	if len(stateDiff) == 0 {
		return nil
	}

	return map[string]any{
		mailboxAddr.Hex(): map[string]any{
			"stateDiff": stateDiff,
		},
	}
}

// MergeStateOverrides merges two state override maps.
func MergeStateOverrides(base, overlay map[string]any) map[string]any {
	if overlay == nil {
		return base
	}
	if base == nil {
		return overlay
	}

	for addr, override := range overlay {
		existing, ok := base[addr]
		if !ok {
			base[addr] = override
			continue
		}

		existingMap, ok1 := existing.(map[string]any)
		overrideMap, ok2 := override.(map[string]any)
		if !ok1 || !ok2 {
			base[addr] = override
			continue
		}

		if state, ok := overrideMap["state"]; ok {
			existingMap["state"] = state
			delete(existingMap, "stateDiff")
		}

		if diff, ok := overrideMap["stateDiff"]; ok {
			overrideDiff := normalizeStateDiff(diff)
			if existingMap["state"] != nil {
				mergedState := normalizeState(existingMap["state"])
				if mergedState == nil {
					mergedState = make(map[string]string)
				}
				for k, v := range overrideDiff {
					mergedState[k] = v
				}
				existingMap["state"] = mergedState
				delete(existingMap, "stateDiff")
			} else {
				existingDiff := normalizeStateDiff(existingMap["stateDiff"])
				if existingDiff == nil {
					existingDiff = make(map[string]string)
				}
				for k, v := range overrideDiff {
					existingDiff[k] = v
				}
				existingMap["stateDiff"] = existingDiff
			}
		}

		for k, v := range overrideMap {
			if k == "state" || k == "stateDiff" {
				continue
			}
			existingMap[k] = v
		}

		base[addr] = existingMap
	}

	return base
}

func normalizeStateDiff(v any) map[string]string {
	switch t := v.(type) {
	case map[string]string:
		return t
	case map[common.Hash]common.Hash:
		out := make(map[string]string, len(t))
		for k, v := range t {
			out[k.Hex()] = v.Hex()
		}
		return out
	case map[string]any:
		out := make(map[string]string, len(t))
		for k, v := range t {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
		return out
	default:
		return nil
	}
}

func normalizeState(v any) map[string]string {
	switch t := v.(type) {
	case map[string]string:
		return t
	case map[string]any:
		out := make(map[string]string, len(t))
		for k, v := range t {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
		return out
	default:
		return nil
	}
}

func mailboxKey(destChainID compose.ChainID, dep mailbox.CrossRollupDependency) (common.Hash, bool) {
	if dep.SessionID == nil {
		return common.Hash{}, false
	}

	srcID := new(big.Int).SetUint64(uint64(dep.SourceChainID))
	destID := new(big.Int).SetUint64(uint64(destChainID))

	buf := make([]byte, 0, 32+32+20+20+32+len(dep.Label))
	buf = append(buf, common.LeftPadBytes(srcID.Bytes(), 32)...)
	buf = append(buf, common.LeftPadBytes(destID.Bytes(), 32)...)
	buf = append(buf, dep.Sender.Bytes()...)
	buf = append(buf, dep.Receiver.Bytes()...)
	buf = append(buf, common.LeftPadBytes(dep.SessionID.Bytes(), 32)...)
	buf = append(buf, dep.Label...)

	return crypto.Keccak256Hash(buf), true
}

func mappingSlot(key common.Hash, slot uint64) common.Hash {
	slotBytes := common.LeftPadBytes(new(big.Int).SetUint64(slot).Bytes(), 32)
	blob := append(key.Bytes(), slotBytes...)
	return crypto.Keccak256Hash(blob)
}

func applyBytesToStateDiff(stateDiff map[string]string, slot common.Hash, data []byte) {
	if len(data) <= 31 {
		stateDiff[slot.Hex()] = encodeShortBytes(data).Hex()
		return
	}

	length := new(big.Int).SetUint64(uint64(len(data))*2 + 1)
	stateDiff[slot.Hex()] = common.BigToHash(length).Hex()

	base := crypto.Keccak256Hash(slot.Bytes())
	baseInt := new(big.Int).SetBytes(base.Bytes())

	chunks := (len(data) + 31) / 32
	for i := 0; i < chunks; i++ {
		start := i * 32
		end := start + 32
		if end > len(data) {
			end = len(data)
		}
		chunk := data[start:end]

		var word common.Hash
		copy(word[:], chunk)

		slotInt := new(big.Int).Add(baseInt, big.NewInt(int64(i)))
		stateDiff[common.BigToHash(slotInt).Hex()] = word.Hex()
	}
}

func encodeShortBytes(data []byte) common.Hash {
	var word common.Hash
	copy(word[:], data)
	word[31] = byte(len(data) * 2)
	return word
}
