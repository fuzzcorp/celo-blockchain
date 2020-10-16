// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package fetcher

import (
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
)

var (
	proof, _     = hex.DecodeString("dummyProof0")
	testMetadata = types.PlumoProofMetadata{
		FirstEpoch:    0,
		LastEpoch:     1,
		VersionNumber: 1,
	}
	testProof = types.PlumoProof{
		Proof:    proof,
		Metadata: testMetadata,
	}
)

// makeProofs creates a list of n proofs, iteratively increasing the proof range by `step`
func makeProofs(n int, step int) ([]types.PlumoProofMetadata, map[types.PlumoProofMetadata]*types.PlumoProof) {
	var proofsMetadata []types.PlumoProofMetadata
	proofs := make(map[types.PlumoProofMetadata]*types.PlumoProof, n)
	for i := 0; i < n; i++ {
		metadata := types.PlumoProofMetadata{
			FirstEpoch:    uint(step * i),
			LastEpoch:     uint(step*i + step),
			VersionNumber: 0,
		}
		proofsMetadata = append(proofsMetadata, metadata)
		proofString := fmt.Sprintf("%s%d", "bc", i)
		if len(proofString)%2 != 0 {
			proofString += "d"
		}
		dummyProofString, _ := hex.DecodeString(proofString)
		dummyProof := &types.PlumoProof{Proof: dummyProofString, Metadata: metadata}
		proofs[metadata] = dummyProof
	}
	return proofsMetadata, proofs
}

// proofFetcherTester is a test simulator for mocking out local proof gathering.
type proofFetcherTester struct {
	proofFetcher *ProofFetcher

	proofsMetadata []types.PlumoProofMetadata                     // Proof metadata belonging to the tester
	proofs         map[types.PlumoProofMetadata]*types.PlumoProof // Proofs belonging to the tester
	drops          map[string]bool                                // Map of peers dropped by the proof fetcher

	lock sync.RWMutex
}

// newProofTester creates a new proof fetcher test mocker.
func newProofTester() *proofFetcherTester {
	var proofsMetadata []types.PlumoProofMetadata
	proofsMetadata = append(proofsMetadata, testMetadata)
	tester := &proofFetcherTester{
		proofsMetadata: nil,
		proofs:         make(map[types.PlumoProofMetadata]*types.PlumoProof),
		drops:          make(map[string]bool),
	}
	tester.proofFetcher = NewProofFetcher(tester.getProof, tester.verifyProof, tester.broadcastProof, tester.insertProofs, tester.dropPeer)
	tester.proofFetcher.Start()

	return tester
}

// getProof retrieves a proof from the tester's storage.
func (pf *proofFetcherTester) getProof(metadata types.PlumoProofMetadata) *types.PlumoProof {
	pf.lock.RLock()
	defer pf.lock.RUnlock()

	return pf.proofs[metadata]
}

// verifyProof is a nop placeholder for the proof verification.
func (pf *proofFetcherTester) verifyProof(proof *types.PlumoProof) error {
	return nil
}

// broadcastProof is a nop placeholder for proof broadcasting.
func (pf *proofFetcherTester) broadcastProof(proof *types.PlumoProof, propagate bool) {
}

// insertProofs injects a new proof into the db.
func (pf *proofFetcherTester) insertProofs(proofs types.PlumoProofs) error {
	pf.lock.Lock()
	defer pf.lock.Unlock()

	for _, proof := range proofs {
		pf.proofsMetadata = append(pf.proofsMetadata, proof.Metadata)
		pf.proofs[proof.Metadata] = proof
	}
	return nil
}

// dropPeer is an emulator for the peer removal, simply accumulating the various
// peers dropped by the fetcher.
func (pf *proofFetcherTester) dropPeer(peer string) {
	pf.lock.Lock()
	defer pf.lock.Unlock()

	pf.drops[peer] = true
}

// makeProofFetcher retrieves a proof fetcher associated with simulated peer.
func (pf *proofFetcherTester) makeProofFetcher(peer string, proofs map[types.PlumoProofMetadata]*types.PlumoProof, drift time.Duration) proofRequesterFn {
	closure := make(map[types.PlumoProofMetadata]*types.PlumoProof)
	for metadata, proof := range proofs {
		closure[metadata] = proof
	}
	// Create a function that returns proofs from the closure
	return func(proofsMetadata []types.PlumoProofMetadata) error {
		// Gather the proofs to return
		proofs := make(types.PlumoProofs, 0, 1)
		for _, metadata := range proofsMetadata {
			if proof, ok := closure[metadata]; ok {
				proofs = append(proofs, proof)
			}
		}
		// Return on a new thread
		go pf.proofFetcher.FilterProofs(peer, proofs, time.Now().Add(drift))

		return nil
	}
}

// verifyProofImportEvent verifies that one single event arrive on an import channel.
func verifyProofImportEvent(t *testing.T, imported chan *types.PlumoProof, arrive bool) {
	if arrive {
		select {
		case <-imported:
		case <-time.After(2 * time.Second):
			t.Fatalf("import timeout")
		}
	} else {
		select {
		case <-imported:
			t.Fatalf("import invoked")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// verifyProofImportCount verifies that exactly count number of events arrive on an
// import hook channel.
func verifyProofImportCount(t *testing.T, imported chan *types.PlumoProof, count int) {
	for i := 0; i < count; i++ {
		select {
		case <-imported:
		case <-time.After(time.Second):
			t.Fatalf("proof %d: import timeout", i+1)
		}
	}
	verifyProofImportDone(t, imported)
}

// verifyProofImportDone verifies that no more events are arriving on an import channel.
func verifyProofImportDone(t *testing.T, imported chan *types.PlumoProof) {
	select {
	case <-imported:
		t.Fatalf("extra proof imported")
	case <-time.After(50 * time.Millisecond):
	}
}

// Tests that a fetcher accepts proof announcements and initiates retrievals for
// them, successfully importing into the local storage.
func TestProofSequentialAnnouncements(t *testing.T) {
	// Create a chain of proofs to import
	targetProofs := 4 * proofLimit
	proofsMetadata, proofs := makeProofs(targetProofs, 1)

	tester := newProofTester()
	proofFetcher := tester.makeProofFetcher("valid", proofs, -gatherSlack)

	// Iteratively announce proofs until all are imported
	imported := make(chan *types.PlumoProof)
	tester.proofFetcher.importedHook = func(proof *types.PlumoProof) { imported <- proof }

	for i := 0; i < len(proofsMetadata); i++ {
		tester.proofFetcher.Notify("valid", proofsMetadata[i], time.Now().Add(-arriveTimeout), proofFetcher)
		verifyProofImportEvent(t, imported, true)
	}
	verifyProofImportDone(t, imported)
}

// Tests that if proofs are announced by multiple peers (or even the same buggy
// peer), they will only get downloaded at most once.
func TestProofConcurrentAnnouncements(t *testing.T) {
	// Create a chain of proofs to import
	targetProofs := 4 * proofLimit
	proofsMetadata, proofs := makeProofs(targetProofs, 1)

	// Assemble a tester with a built in counter for the requests
	tester := newProofTester()
	firstProofFetcher := tester.makeProofFetcher("first", proofs, -gatherSlack)
	secondProofFetcher := tester.makeProofFetcher("second", proofs, -gatherSlack)

	counter := uint32(0)
	firstProofWrapper := func(metadata []types.PlumoProofMetadata) error {
		atomic.AddUint32(&counter, 1)
		return firstProofFetcher(metadata)
	}
	secondProofWrapper := func(metadata []types.PlumoProofMetadata) error {
		atomic.AddUint32(&counter, 1)
		return secondProofFetcher(metadata)
	}
	// Iteratively announce proofs until all are imported
	imported := make(chan *types.PlumoProof)
	tester.proofFetcher.importedHook = func(proof *types.PlumoProof) { imported <- proof }

	for i := 0; i < len(proofsMetadata); i++ {
		tester.proofFetcher.Notify("first", proofsMetadata[i], time.Now().Add(-arriveTimeout), firstProofWrapper)
		tester.proofFetcher.Notify("second", proofsMetadata[i], time.Now().Add(-arriveTimeout+time.Millisecond), secondProofWrapper)
		tester.proofFetcher.Notify("second", proofsMetadata[i], time.Now().Add(-arriveTimeout-time.Millisecond), secondProofWrapper)
		verifyProofImportEvent(t, imported, true)
	}
	verifyProofImportDone(t, imported)

	// Make sure no proofs were retrieved twice
	if int(counter) != targetProofs {
		t.Fatalf("retrieval count mismatch: have %v, want %v", counter, targetProofs)
	}
}

// Tests that announcements arriving while a previous is being fetched still
// results in a valid import.
func TestProofOverlappingAnnouncements(t *testing.T) {
	// Create a chain of blocks to import
	targetProofs := 4 * proofLimit
	proofsMetadata, proofs := makeProofs(targetProofs, 1)

	tester := newProofTester()
	proofFetcher := tester.makeProofFetcher("valid", proofs, -gatherSlack)

	// Iteratively announce proofs, but overlap them continuously
	overlap := 16
	imported := make(chan *types.PlumoProof, len(proofsMetadata)-1)
	for i := 0; i < overlap; i++ {
		imported <- nil
	}
	tester.proofFetcher.importedHook = func(proof *types.PlumoProof) { imported <- proof }

	for i := len(proofsMetadata) - 1; i >= 0; i-- {
		tester.proofFetcher.Notify("valid", proofsMetadata[i], time.Now().Add(-arriveTimeout), proofFetcher)
		select {
		case <-imported:
		case <-time.After(time.Second):
			t.Fatalf("proof %d: import timeout", len(proofsMetadata)-i)
		}
	}
	// Wait for all the imports to complete and check count
	verifyProofImportCount(t, imported, overlap)
}

// Tests that announces already being retrieved will not be duplicated.
func TestProofPendingDeduplication(t *testing.T) {
	// Create a hash and corresponding block
	// Create poof and emtadata
	proofsMetadata, proofs := makeProofs(1, 1)

	// Assemble a tester with a built in counter and delayed fetcher
	tester := newProofTester()
	proofFetcher := tester.makeProofFetcher("repeater", proofs, -gatherSlack)

	delay := 50 * time.Millisecond
	counter := uint32(0)
	proofWrapper := func(metadata []types.PlumoProofMetadata) error {
		atomic.AddUint32(&counter, 1)

		// Simulate a long running fetch
		go func() {
			time.Sleep(delay)
			proofFetcher(metadata)
		}()
		return nil
	}
	// Announce the same proof many times until it's fetched (wait for any pending ops)
	for tester.getProof(proofsMetadata[0]) == nil {
		tester.proofFetcher.Notify("repeater", proofsMetadata[0], time.Now().Add(-arriveTimeout), proofWrapper)
		time.Sleep(time.Millisecond)
	}
	time.Sleep(delay)

	// Check that all proofs were imported and none fetched twice
	if imported := len(tester.proofs); imported != 1 {
		t.Fatalf("synchronised proof mismatch: have %v, want %v", imported, 1)
	}
	if int(counter) != 1 {
		t.Fatalf("retrieval count mismatch: have %v, want %v", counter, 1)
	}
}

// Tests that announcements retrieved in a random order are cached and eventually
// imported when all the gaps are filled in.
func TestProofRandomArrivalImport(t *testing.T) {
	// Create a list of proofs to import, and choose one to delay
	targetProofs := maxQueueDist
	proofsMetadata, proofs := makeProofs(targetProofs, 1)
	skip := targetProofs / 2

	tester := newProofTester()
	proofFetcher := tester.makeProofFetcher("valid", proofs, -gatherSlack)

	// Iteratively announce proofs, skipping one entry
	imported := make(chan *types.PlumoProof, len(proofsMetadata)-1)
	tester.proofFetcher.importedHook = func(proof *types.PlumoProof) { imported <- proof }

	for i := len(proofsMetadata) - 1; i >= 0; i-- {
		if i != skip {
			tester.proofFetcher.Notify("valid", proofsMetadata[i], time.Now().Add(-arriveTimeout), proofFetcher)
			time.Sleep(time.Millisecond)
		}
	}
	// Finally announce the skipped entry and check full import
	tester.proofFetcher.Notify("valid", proofsMetadata[skip], time.Now().Add(-arriveTimeout), proofFetcher)
	verifyProofImportCount(t, imported, len(proofsMetadata))
}

// Tests that direct proof enqueues (due to proof propagation vs. metadata announce)
// are correctly schedule, filling and import queue gaps.
func TestProofQueueGapFill(t *testing.T) {
	// Create a list of proofs to import, and choose one to not announce at all
	targetProofs := maxQueueDist
	proofsMetadata, proofs := makeProofs(targetProofs, 1)
	skip := targetProofs / 2

	tester := newProofTester()
	proofFetcher := tester.makeProofFetcher("valid", proofs, -gatherSlack)

	// Iteratively announce proofs, skipping one entry
	imported := make(chan *types.PlumoProof, len(proofsMetadata)-1)
	tester.proofFetcher.importedHook = func(proof *types.PlumoProof) { imported <- proof }

	for i := len(proofsMetadata) - 1; i >= 0; i-- {
		if i != skip {
			tester.proofFetcher.Notify("valid", proofsMetadata[i], time.Now().Add(-arriveTimeout), proofFetcher)
			time.Sleep(time.Millisecond)
		}
	}
	// Fill the missing proof directly as if propagated
	tester.proofFetcher.Enqueue("valid", proofs[proofsMetadata[skip]])
	verifyProofImportCount(t, imported, len(proofsMetadata))
}

// Tests that proofs arriving from various sources (multiple propagations, metadata
// announces, etc) do not get scheduled for import multiple times.
func TestProofImportDeduplication(t *testing.T) {
	// Create two proofs to import (one for duplication, the other for stalling)
	proofsMetadata, proofs := makeProofs(2, 1)

	// Create the tester and wrap the importer with a counter
	tester := newProofTester()
	proofFetcher := tester.makeProofFetcher("valid", proofs, -gatherSlack)

	counter := uint32(0)
	tester.proofFetcher.insertProofs = func(proofs types.PlumoProofs) error {
		atomic.AddUint32(&counter, uint32(len(proofs)))
		return tester.insertProofs(proofs)
	}
	// Instrument the fetching and imported events
	fetching := make(chan []types.PlumoProofMetadata)
	imported := make(chan *types.PlumoProof, len(proofsMetadata)-1)
	tester.proofFetcher.fetchingHook = func(proofsMetadata []types.PlumoProofMetadata) { fetching <- proofsMetadata }
	tester.proofFetcher.importedHook = func(proof *types.PlumoProof) { imported <- proof }

	// Announce the duplicating block, wait for retrieval, and also propagate directly
	tester.proofFetcher.Notify("valid", proofsMetadata[0], time.Now().Add(-arriveTimeout), proofFetcher)
	<-fetching

	tester.proofFetcher.Enqueue("valid", proofs[proofsMetadata[0]])
	tester.proofFetcher.Enqueue("valid", proofs[proofsMetadata[0]])
	tester.proofFetcher.Enqueue("valid", proofs[proofsMetadata[0]])

	// Fill the missing proof directly as if propagated, and check import uniqueness
	tester.proofFetcher.Enqueue("valid", proofs[proofsMetadata[1]])
	verifyProofImportCount(t, imported, 2)

	if counter != 2 {
		t.Fatalf("import invocation count mismatch: have %v, want %v", counter, 2)
	}
}

// TODO: TestInvalidMetadata announcement
// Tests that peers announcing proofs with invalid numbers (i.e. not matching
// the headers provided afterwards) get dropped as malicious.
// func TestInvalidNumberAnnouncement(t *testing.T) {
// 	// Create a single block to import and check numbers against
// 	proofsMetadata, proofs := makeProofs(1, 0, genesis)

// 	tester := newTester()
// 	badHeaderFetcher := tester.makeHeaderFetcher("bad", proofs, -gatherSlack)
// 	badBodyFetcher := tester.makeBodyFetcher("bad", proofs, 0)

// 	imported := make(chan *types.Block)
// 	tester.fetcher.importedHook = func(block *types.Block) { imported <- block }

// 	// Announce a block with a bad number, check for immediate drop
// 	tester.fetcher.Notify("bad", proofsMetadata[0], 2, time.Now().Add(-arriveTimeout), badHeaderFetcher, badBodyFetcher)
// 	verifyImportEvent(t, imported, false)

// 	tester.lock.RLock()
// 	dropped := tester.drops["bad"]
// 	tester.lock.RUnlock()

// 	if !dropped {
// 		t.Fatalf("peer with invalid numbered announcement not dropped")
// 	}

// 	goodHeaderFetcher := tester.makeHeaderFetcher("good", proofs, -gatherSlack)
// 	goodBodyFetcher := tester.makeBodyFetcher("good", proofs, 0)
// 	// Make sure a good announcement passes without a drop
// 	tester.fetcher.Notify("good", proofsMetadata[0], 1, time.Now().Add(-arriveTimeout), goodHeaderFetcher, goodBodyFetcher)
// 	verifyImportEvent(t, imported, true)

// 	tester.lock.RLock()
// 	dropped = tester.drops["good"]
// 	tester.lock.RUnlock()

// 	if dropped {
// 		t.Fatalf("peer with valid numbered announcement dropped")
// 	}
// 	verifyImportDone(t, imported)
// }

// Tests that a peer is unable to use unbounded memory with sending infinite
// proof announcements to a node, but that even in the face of such an attack,
// the fetcher remains operational.
// func TestProofMemoryExhaustionAttack(t *testing.T) {
// 	// Create a tester with instrumented import hooks
// 	tester := newProofTester()

// 	imported, announces := make(chan *types.PlumoProof), int32(0)
// 	tester.proofFetcher.importedHook = func(proof *types.PlumoProof) { imported <- proof }
// 	tester.proofFetcher.announceChangeHook = func(metadata types.PlumoProofMetadata, added bool) {
// 		if added {
// 			atomic.AddInt32(&announces, 1)
// 		} else {
// 			atomic.AddInt32(&announces, -1)
// 		}
// 	}
// 	// Create a valid chain and an infinite junk chain
// 	targetBlocks := proofLimit + 2*maxQueueDist
// 	proofsMetadata, proofs := makeProofs(targetBlocks, 0, genesis)
// 	validHeaderFetcher := tester.makeHeaderFetcher("valid", proofs, -gatherSlack)
// 	validBodyFetcher := tester.makeBodyFetcher("valid", proofs, 0)

// 	attack, _ := makeProofs(targetBlocks, 0, unknownBlock)
// 	attackerHeaderFetcher := tester.makeHeaderFetcher("attacker", nil, -gatherSlack)
// 	attackerBodyFetcher := tester.makeBodyFetcher("attacker", nil, 0)

// 	// Feed the tester a huge hashset from the attacker, and a limited from the valid peer
// 	for i := 0; i < len(attack); i++ {
// 		if i < maxQueueDist {
// 			tester.fetcher.Notify("valid", proofsMetadata[len(proofsMetadata)-2-i], uint64(i+1), time.Now(), validHeaderFetcher, validBodyFetcher)
// 		}
// 		tester.fetcher.Notify("attacker", attack[i], 1 /* don't distance drop */, time.Now(), attackerHeaderFetcher, attackerBodyFetcher)
// 	}
// 	if count := atomic.LoadInt32(&announces); count != proofLimit+maxQueueDist {
// 		t.Fatalf("queued announce count mismatch: have %d, want %d", count, proofLimit+maxQueueDist)
// 	}
// 	// Wait for fetches to complete
// 	verifyImportCount(t, imported, maxQueueDist)

// 	// Feed the remaining valid proofsMetadata to ensure DOS protection state remains clean
// 	for i := len(proofsMetadata) - maxQueueDist - 2; i >= 0; i-- {
// 		tester.fetcher.Notify("valid", proofsMetadata[i], uint64(len(proofsMetadata)-i-1), time.Now().Add(-arriveTimeout), validHeaderFetcher, validBodyFetcher)
// 		verifyImportEvent(t, imported, true)
// 	}
// 	verifyImportDone(t, imported)
// }

// Tests that proofs sent to the fetcher (either through propagation or via hash
// announces and retrievals) don't pile up indefinitely, exhausting available
// system memory.
// func TestBlockMemoryExhaustionAttack(t *testing.T) {
// 	// Create a tester with instrumented import hooks
// 	tester := newTester()

// 	imported, enqueued := make(chan *types.Block), int32(0)
// 	tester.fetcher.importedHook = func(block *types.Block) { imported <- block }
// 	tester.fetcher.queueChangeHook = func(hash common.Hash, added bool) {
// 		if added {
// 			atomic.AddInt32(&enqueued, 1)
// 		} else {
// 			atomic.AddInt32(&enqueued, -1)
// 		}
// 	}
// 	// Create a valid chain and a batch of dangling (but in range) proofs
// 	targetBlocks := proofLimit + 2*maxQueueDist
// 	proofsMetadata, proofs := makeProofs(targetBlocks, 0, genesis)
// 	attack := make(map[common.Hash]*types.Block)
// 	for i := byte(0); len(attack) < blockLimit+2*maxQueueDist; i++ {
// 		proofsMetadata, proofs := makeProofs(maxQueueDist-1, i, unknownBlock)
// 		for _, hash := range proofsMetadata[:maxQueueDist-2] {
// 			attack[hash] = proofs[hash]
// 		}
// 	}
// 	// Try to feed all the attacker proofs make sure only a limited batch is accepted
// 	for _, block := range attack {
// 		tester.fetcher.Enqueue("attacker", block)
// 	}
// 	time.Sleep(200 * time.Millisecond)
// 	if queued := atomic.LoadInt32(&enqueued); queued != blockLimit {
// 		t.Fatalf("queued block count mismatch: have %d, want %d", queued, blockLimit)
// 	}
// 	// Queue up a batch of valid proofs, and check that a new peer is allowed to do so
// 	for i := 0; i < maxQueueDist-1; i++ {
// 		tester.fetcher.Enqueue("valid", proofs[proofsMetadata[len(proofsMetadata)-3-i]])
// 	}
// 	time.Sleep(100 * time.Millisecond)
// 	if queued := atomic.LoadInt32(&enqueued); queued != blockLimit+maxQueueDist-1 {
// 		t.Fatalf("queued block count mismatch: have %d, want %d", queued, blockLimit+maxQueueDist-1)
// 	}
// 	// Insert the missing piece (and sanity check the import)
// 	tester.fetcher.Enqueue("valid", proofs[proofsMetadata[len(proofsMetadata)-2]])
// 	verifyImportCount(t, imported, maxQueueDist)

// 	// Insert the remaining proofs in chunks to ensure clean DOS protection
// 	for i := maxQueueDist; i < len(proofsMetadata)-1; i++ {
// 		tester.fetcher.Enqueue("valid", proofs[proofsMetadata[len(proofsMetadata)-2-i]])
// 		verifyImportEvent(t, imported, true)
// 	}
// 	verifyImportDone(t, imported)
// }