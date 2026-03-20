package packet

import (
	"context"
	"fmt"
	"log"

	"github.com/florianl/go-nfqueue"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

type Handler struct {
	queueNum uint16
	strategy DPIBypassStrategy
	nfq      *nfqueue.Nfqueue
}

func New(queueNum uint16, strategy DPIBypassStrategy) *Handler {
	return &Handler{
		queueNum: queueNum,
		strategy: strategy,
	}
}

func (h *Handler) Start(ctx context.Context) error {
	config := nfqueue.Config{
		NfQueue:      h.queueNum,
		MaxPacketLen: 0xFFFF,
		MaxQueueLen:  0xFF,
		Copymode:     nfqueue.NfQnlCopyPacket,
		WriteTimeout: 15,
	}

	nfq, err := nfqueue.Open(&config)
	if err != nil {
		return fmt.Errorf("failed to open nfqueue: %w", err)
	}
	h.nfq = nfq

	hookFn := func(a nfqueue.Attribute) int {
		return h.processPacket(a)
	}

	if err := nfq.RegisterWithErrorFunc(ctx, hookFn, func(err error) int {
		if ctx.Err() != nil {
			return -1
		}
		log.Panicf("Error in NFQUEUE: %v", err)
		return 0
	}); err != nil {
		return fmt.Errorf("failed to register nfqueue hook: %w", err)
	}

	log.Printf("Started listening on NFQUEUE number %d", h.queueNum)

	<-ctx.Done()
	return nfq.Close()
}

func (h *Handler) processPacket(a nfqueue.Attribute) int {
	id := *a.PacketID
	rawData := *a.Payload

	packet := gopacket.NewPacket(rawData, layers.LayerTypeIPv4, gopacket.Default)

	idLayer := packet.Layer(layers.LayerTypeIPv4)
	udoLayer := packet.Layer(layers.LayerTypeUDP)

	if idLayer == nil || udoLayer == nil {
		h.nfq.SetVerdict(id, nfqueue.NfAccept)
		return 0
	}

	ip, _ := idLayer.(*layers.IPv4)
	udp, _ := udoLayer.(*layers.UDP)

	result := ObfuscateUDP(udp.Payload, h.strategy)

	if !result.Changed {
		h.nfq.SetVerdict(id, nfqueue.NfAccept)
		return 0
	}

	if len(result.Packets) == 1 {
		modifiedPacket, err := rebuildUDPPacket(ip, udp, result.Packets[0])
		if err != nil {
			log.Printf("error in packat re build: %v", err)
			h.nfq.SetVerdict(id, nfqueue.NfDrop)
			return 0
		}
		h.nfq.SetVerdictModPacket(id, nfqueue.NfAccept, modifiedPacket)
		return 0
	}

	firstPacket, err := rebuildUDPPacket(ip, udp, result.Packets[0])
	if err != nil {
		log.Printf("error in packat re build: %v", err)
		h.nfq.SetVerdict(id, nfqueue.NfDrop)
		return 0
	}

	h.nfq.SetVerdictModPacket(id, nfqueue.NfAccept, firstPacket)

	log.Printf("UDP packet modified: %d bytes -> %d bytes",
		len(udp.Payload), len(result.Packets[0]))

	return 0
}

func rebuildUDPPacket(ip *layers.IPv4, udp *layers.UDP, newPayload []byte) ([]byte, error) {
	newUDP := *udp
	newUDP.Payload = newPayload

	newUDP.SetNetworkLayerForChecksum(ip)

	newIP := *ip

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	err := gopacket.SerializeLayers(buf, opts,
		&newIP,
		&newUDP,
		gopacket.Payload(newPayload),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize packet: %w", err)
	}
	return buf.Bytes(), nil
}
