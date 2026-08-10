package frontier

import "testing"

type stubStateStore struct {
	queued     []string
	processing []string
	done       []string
}

func (s *stubStateStore) QueueURL(url string) {
	s.queued = append(s.queued, url)
}

func (s *stubStateStore) MarkProcessing(url string) {
	s.processing = append(s.processing, url)
}

func (s *stubStateStore) MarkDone(url string) {
	s.done = append(s.done, url)
}

func TestFrontierStateHooksAndNormalization(t *testing.T) {
	stub := &stubStateStore{}
	f := NewWithStateStore(stub)

	f.Add("https://example.com:443/path#frag")

	if len(stub.queued) != 1 || stub.queued[0] != "https://example.com/path" {
		t.Fatalf("unexpected queued urls: %#v", stub.queued)
	}

	u, ok := f.Next()
	if !ok {
		t.Fatal("expected queued url")
	}
	if u != "https://example.com/path" {
		t.Fatalf("unexpected next url: %s", u)
	}
	if len(stub.processing) != 1 || stub.processing[0] != u {
		t.Fatalf("unexpected processing urls: %#v", stub.processing)
	}

	f.Done(u)
	if len(stub.done) != 1 || stub.done[0] != u {
		t.Fatalf("unexpected done urls: %#v", stub.done)
	}
}

func TestMarkSeenSkipsRequeue(t *testing.T) {
	stub := &stubStateStore{}
	f := NewWithStateStore(stub)

	f.MarkSeen("https://example.com/path")
	f.Add("https://example.com/path")

	if got := f.QueueDepth(); got != 0 {
		t.Fatalf("expected empty queue, got %d", got)
	}
	if len(stub.queued) != 0 {
		t.Fatalf("expected no queue calls, got %#v", stub.queued)
	}
}
