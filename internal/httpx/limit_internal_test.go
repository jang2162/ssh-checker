package httpx

import (
	"strconv"
	"testing"
	"time"
)

func TestKeyedExhaustedSurvivesFullTable(t *testing.T) {
	k := NewKeyed(time.Hour, 1)
	k.Allow("victim")
	if !k.Exhausted("victim") {
		t.Fatal("victim should be exhausted")
	}
	for i := 0; i < maxKeys+5; i++ {
		k.Exhausted("probe" + strconv.Itoa(i))
		k.Allow("other" + strconv.Itoa(i))
	}
	if !k.Exhausted("victim") {
		t.Fatal("exhausted bucket was wiped")
	}
}

func TestKeyedExhaustedDoesNotInsert(t *testing.T) {
	k := NewKeyed(time.Hour, 1)
	if k.Exhausted("unknown") {
		t.Fatal("unknown key reported exhausted")
	}
	if len(k.m) != 0 {
		t.Fatalf("Exhausted inserted an entry: %d", len(k.m))
	}
}

func TestKeyedFullTableDeniesNewKey(t *testing.T) {
	k := NewKeyed(time.Hour, 1)
	k.max = 3
	for _, key := range []string{"a", "b", "c"} {
		if !k.Allow(key) {
			t.Fatalf("%s denied", key)
		}
	}
	if k.Allow("d") {
		t.Fatal("new key admitted while table full and nothing refilled")
	}
	if !k.Exhausted("a") {
		t.Fatal("existing bucket was evicted")
	}
}

func TestKeyedRefilledBucketsAreEvicted(t *testing.T) {
	k := NewKeyed(10*time.Millisecond, 2)
	k.max = 3
	for _, key := range []string{"a", "b", "c"} {
		k.Allow(key)
	}
	time.Sleep(60 * time.Millisecond) // burst*interval(20ms)보다 충분히 길게
	if !k.Allow("d") {
		t.Fatal("new key denied after buckets refilled")
	}
}
