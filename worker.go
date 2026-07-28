package main

import (
	"log"
)

// MatchJob is a unit of work: "find and rank carriers for this shipment ID."
// Matching runs async rather than inline on the HTTP request, mirroring the
// JD's mention of background jobs / Kafka-backed workflows for dispatch —
// this project uses an in-process channel + worker pool instead of a real
// queue (BullMQ/Kafka), since standing up Redis or Kafka is out of scope
// for a project meant to run with zero external services. Swapping this
// channel for a Kafka consumer or BullMQ worker would only mean changing
// how MatchJob gets enqueued, not the Process() logic itself.
type MatchJob struct {
	ShipmentID string
	ResultCh   chan []MatchResult // caller reads the ranked results here
}

// MatchWorkerPool runs N goroutines pulling jobs off a shared channel.
type MatchWorkerPool struct {
	jobs  chan MatchJob
	store Store
}

func NewMatchWorkerPool(store Store, numWorkers int, queueSize int) *MatchWorkerPool {
	pool := &MatchWorkerPool{
		jobs:  make(chan MatchJob, queueSize),
		store: store,
	}
	for i := 0; i < numWorkers; i++ {
		go pool.runWorker(i)
	}
	return pool
}

func (p *MatchWorkerPool) runWorker(id int) {
	for job := range p.jobs {
		shipment, err := p.store.GetShipment(job.ShipmentID)
		if err != nil {
			log.Printf("worker %d: shipment %s not found: %v", id, job.ShipmentID, err)
			job.ResultCh <- nil
			close(job.ResultCh)
			continue
		}

		_ = p.store.UpdateShipmentStatus(job.ShipmentID, "matching")

		carriers := p.store.ListCarriers()
		ranked := RankCarriers(carriers, shipment)

		status := "matched"
		if len(ranked) == 0 || !ranked[0].Feasible {
			status = "no_match"
		}
		_ = p.store.UpdateShipmentStatus(job.ShipmentID, status)

		job.ResultCh <- ranked
		close(job.ResultCh)
	}
}

// Submit enqueues a match job and returns a channel the caller can block on
// (or select with a timeout on) to get the ranked results.
func (p *MatchWorkerPool) Submit(shipmentID string) chan []MatchResult {
	resultCh := make(chan []MatchResult, 1)
	p.jobs <- MatchJob{ShipmentID: shipmentID, ResultCh: resultCh}
	return resultCh
}
