package favroapi

import (
	"slices"
	"sync"
)

// backendIDsCap bounds the remembered listings. A caller pages through a
// few listings at a time, and one this process forgot is still served,
// without the header, from page and requestId.
const backendIDsCap = 64

// backendIDs maps a listing's requestId to the X-Favro-Backend-Identifier
// its first page came back with, so the listing's later pages reach the
// same Favro process (https://favro.com/developer/#routing). The zero
// value is ready to use, and the entry seen longest ago goes first when it
// is full.
type backendIDs struct {
	mu    sync.Mutex
	byID  map[string]string
	order []string
}

func (b *backendIDs) get(requestID string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byID[requestID]
}

func (b *backendIDs) put(requestID, backend string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byID == nil {
		b.byID = map[string]string{}
	}
	// A listing seen again moves to the back: it is still being paged.
	b.order = slices.DeleteFunc(b.order, func(id string) bool { return id == requestID })
	b.order = append(b.order, requestID)
	b.byID[requestID] = backend
	if len(b.order) > backendIDsCap {
		delete(b.byID, b.order[0])
		b.order = b.order[1:]
	}
}
