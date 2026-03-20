package packet

import (
	"crypto/rand"
	"encoding/binary"
	"math/big"
)

type DPIBypassStrategy int

const (
	StrategyFragmentation DPIBypassStrategy = iota
	StrategyPadding
	StrategyFakeTTL
	StrategyCombined
)

type ObfuscationResult struct {
	Packets [][]byte
	Changed bool
}

func ObfuscateUDP(payload []byte, strategy DPIBypassStrategy) ObfuscationResult {
	if len(payload) < 8 {
		return ObfuscationResult{Packets: [][]byte{payload}, Changed: false}
	}

	switch strategy {
	case StrategyFragmentation:
		return fragmentPayload(payload)
	case StrategyPadding:
		return addJunkPadding(payload)
	case StrategyCombined:
		padded := addJunkPadding(payload)
		if len(padded.Packets) > 0 {
			return fragmentPayload(padded.Packets[0])
		}
		return padded
	default:
		return ObfuscationResult{Packets: [][]byte{payload}, Changed: false}
	}
}

func fragmentPayload(payload []byte) ObfuscationResult {
	if len(payload) < 16 {
		return ObfuscationResult{Packets: [][]byte{payload}, Changed: false}
	}

	splitPoint := len(payload)/2 + randomInt(-4, 4)
	if splitPoint < 4 {
		splitPoint = 4
	}

	if splitPoint > len(payload) {
		splitPoint = len(payload) - 4
	}

	part1 := make([]byte, splitPoint)
	part2 := make([]byte, len(payload)-splitPoint)
	copy(part1, payload[:splitPoint])
	copy(part2, payload[splitPoint:])

	return ObfuscationResult{
		Packets: [][]byte{part1, part2},
		Changed: true,
	}
}

func addJunkPadding(payload []byte) ObfuscationResult {
	if len(payload) < 12 {
		return ObfuscationResult{Packets: [][]byte{payload}, Changed: false}
	}
	junkSize := 4 + randomInt(0, 8)
	junk := make([]byte, junkSize)
	rand.Read(junk)

	header := payload[:12]
	rest := payload[12:]

	newPayload := make([]byte, 0, len(payload)+junkSize+4)
	newPayload = append(newPayload, header...)

	marker := make([]byte, 4)
	binary.BigEndian.PutUint16(marker[0:2], 0xBEDE) // RTP extension magic
	binary.BigEndian.PutUint16(marker[2:4], uint16(junkSize/4))
	newPayload = append(newPayload, marker...)
	newPayload = append(newPayload, junk...)
	newPayload = append(newPayload, rest...)

	if len(newPayload) > 0 {
		newPayload[0] |= 0x10
	}

	return ObfuscationResult{
		Packets: [][]byte{newPayload},
		Changed: true,
	}
}

func randomInt(min, max int) int {
	if min >= max {
		return min
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max-min)))
	return int(n.Int64()) + min
}
