package notify

import "testing"

func TestPreWrapsEscapedMessage(t *testing.T) {
	in := "a < b\nline2"
	got := Pre(in)
	want := "<pre>a &lt; b\nline2</pre>"
	if got != want {
		t.Fatalf("unexpected pre output: got=%q want=%q", got, want)
	}
}

func TestNewTelegramFromConfigDisabledWithoutTokenChat(t *testing.T) {
	tg := NewTelegramFromConfig(map[string]string{})
	if tg == nil {
		t.Fatal("expected telegram instance")
	}
	if tg.Enabled() {
		t.Fatal("expected disabled telegram when token/chat are missing")
	}
	tg.Stop()
}
