package bbolt

import (
	"encoding/hex"
	"fmt"
)

// Check performs several consistency checks on the database for this transaction.
// An error is returned if any inconsistency is found.
//
// It can be safely run concurrently on a writable transaction. However, this
// incurs a high cost for large databases and databases with a lot of subbuckets
// because of caching. This overhead can be removed if running on a read-only
// transaction, however, it is not safe to execute other writer transactions at
// the same time.
func (tx *Tx) Check(keyValueStringer KeyValueStringer) <-chan error {
	ch := make(chan error)
	go tx.check(keyValueStringer, ch)
	return ch
}

func (tx *Tx) check(keyValueStringer KeyValueStringer, ch chan error) {
	// Force loading free list if opened in ReadOnly mode.
	tx.db.loadFreelist()

	// Check if any pages are double freed.
	freed := make(map[pgid]bool)
	all := make([]pgid, tx.db.freelist.count())
	tx.db.freelist.copyall(all)
	for _, id := range all {
		if freed[id] {
			ch <- fmt.Errorf("page %d: already freed", id)
		}
		freed[id] = true
	}

	// Track every reachable page.
	reachable := make(map[pgid]*page)
	reachable[0] = tx.page(0) // meta0
	reachable[1] = tx.page(1) // meta1
	if tx.meta.freelist != pgidNoFreelist {
		for i := uint32(0); i <= tx.page(tx.meta.freelist).overflow; i++ {
			reachable[tx.meta.freelist+pgid(i)] = tx.page(tx.meta.freelist)
		}
	}

	// Recursively check buckets.
	tx.checkBucket(&tx.root, reachable, freed, keyValueStringer, ch)

	// Ensure all pages below high water mark are either reachable or freed.
	for i := pgid(0); i < tx.meta.pgid; i++ {
		_, isReachable := reachable[i]
		if !isReachable && !freed[i] {
			ch <- fmt.Errorf("page %d: unreachable unfreed", int(i))
		}
	}

	// Close the channel to signal completion.
	close(ch)
}

func (tx *Tx) checkBucket(b *Bucket, reachable map[pgid]*page, freed map[pgid]bool,
	keyValueStringer KeyValueStringer, ch chan error) {
	// Ignore inline buckets.
	if b.root == 0 {
		return
	}

	// Check every page used by this bucket.
	b.tx.forEachPage(b.root, 0, func(p *page, _ int) {
		if p.id > tx.meta.pgid {
			ch <- fmt.Errorf("page %d: out of bounds: %d", int(p.id), int(b.tx.meta.pgid))
		}

		// Ensure each page is only referenced once.
		for i := pgid(0); i <= pgid(p.overflow); i++ {
			var id = p.id + i
			if _, ok := reachable[id]; ok {
				ch <- fmt.Errorf("page %d: multiple references", int(id))
			}
			reachable[id] = p
		}

		// We should only encounter un-freed leaf and branch pages.
		if freed[p.id] {
			ch <- fmt.Errorf("page %d: reachable freed", int(p.id))
		} else if (p.flags&branchPageFlag) == 0 && (p.flags&leafPageFlag) == 0 {
			ch <- fmt.Errorf("page %d: invalid type: %s", int(p.id), p.typ())
		}
	})

	tx.recursivelyCheckPages(b.root, keyValueStringer.KeyToString, ch)

	// Check each bucket within this bucket.
	_ = b.ForEach(func(k, v []byte) error {
		if child := b.Bucket(k); child != nil {
			tx.checkBucket(child, reachable, freed, keyValueStringer, ch)
		}
		return nil
	})
}

// Recursive checker confirms database consistency with respect to b-tree
// key order constraints:
//  - keys on pages must be sorted
//  - keys on children pages are between 2 consecutive keys on parent
// branch page).
func (tx *Tx) recursivelyCheckPages(pgid pgid, keyToString func([]byte) string, ch chan error) (maxKeyInSubtree []byte) {
	return tx.recursivelyCheckPagesInternal(pgid, nil, nil, nil, keyToString, ch)
}

func (tx *Tx) recursivelyCheckPagesInternal(pgid pgid, minKeyClosed, maxKeyOpen []byte, pagesStack []pgid,
	keyToString func([]byte) string, ch chan error) (maxKeyInSubtree []byte) {
	p := tx.page(pgid)
	pagesStack = append(pagesStack, pgid)

	switch {
	case p.flags&branchPageFlag != 0:
		runningMin := minKeyClosed
		for i := range p.branchPageElements() {
			elem := p.branchPageElement(uint16(i))
			if i == 0 && runningMin != nil && compareKeys(runningMin, elem.key()) > 0 {
				ch <- fmt.Errorf("key (%d, %s) on the branch page(%d) needs to be >= to the index in the ancestor. Pages stack: %v",
					i, keyToString(elem.key()), pgid, pagesStack)
			}

			if maxKeyOpen != nil && compareKeys(elem.key(), maxKeyOpen) >= 0 {
				ch <- fmt.Errorf("key (%d: %s) on the branch page(%d) needs to be < than key of the next element reachable from the ancestor (%v). Pages stack: %v",
					i, keyToString(elem.key()), pgid, keyToString(maxKeyOpen), pagesStack)
			}

			var maxKey []byte
			if i < len(p.branchPageElements())-1 {
				maxKey = p.branchPageElement(uint16(i + 1)).key()
			} else {
				maxKey = maxKeyOpen
			}
			maxKeyInSubtree = tx.recursivelyCheckPagesInternal(elem.pgid, elem.key(), maxKey, pagesStack, keyToString, ch)
			runningMin = maxKeyInSubtree
		}
		return
	case p.flags&leafPageFlag != 0:
		runningMin := minKeyClosed
		for i := range p.leafPageElements() {
			elem := p.leafPageElement(uint16(i))
			if i == 0 && runningMin != nil && compareKeys(runningMin, elem.key()) > 0 {
				ch <- fmt.Errorf("key (%d: %s) on leaf page(%d) needs to be >= to the key in the ancestor. Stack: %v",
					i, keyToString(elem.key()), pgid, pagesStack)
			}
			if i > 0 && compareKeys(runningMin, elem.key()) > 0 {
				ch <- fmt.Errorf("key (%d: %s) on leaf page(%d) needs to be > (found <) than previous element (%s). Stack: %v",
					i, keyToString(elem.key()), pgid, keyToString(runningMin), pagesStack)
			}
			if i > 0 && compareKeys(runningMin, elem.key()) == 0 {
				ch <- fmt.Errorf("key (%d: %s) on leaf page(%d) needs to be > (found =) than previous element (%s). Stack: %v",
					i, keyToString(elem.key()), pgid, keyToString(runningMin), pagesStack)
			}
			if maxKeyOpen != nil && compareKeys(elem.key(), maxKeyOpen) >= 0 {
				ch <- fmt.Errorf("key (%d, %s) on leaf page(%d) needs to be < than key of the next element in ancestor (%s). Pages stack: %v",
					i, keyToString(elem.key()), pgid, keyToString(maxKeyOpen), pagesStack)
			}
			runningMin = elem.key()
		}
		if p.count > 0 {
			return p.leafPageElement(p.count - 1).key()
		}
	default:
		ch <- fmt.Errorf("unexpected page type for pgid:%d", pgid)
	}
	return nil
}

// ===========================================================================================

// KeyValueStringer allows to prepare human-readable diagnostic messages.
type KeyValueStringer interface {
	KeyToString([]byte) string
	ValueToString([]byte) string
}

// HexKeyValueStringer serializes both key & value to hex representation.
func HexKeyValueStringer() KeyValueStringer {
	return hexKeyValueStringer{}
}

type hexKeyValueStringer struct{}

func (_ hexKeyValueStringer) KeyToString(key []byte) string {
	return hex.EncodeToString(key)
}

func (_ hexKeyValueStringer) ValueToString(value []byte) string {
	return hex.EncodeToString(value)
}
