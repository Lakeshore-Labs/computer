//go:build unix

package agent

import "testing"

func TestAConversationIsHeldByOneRunAtATime(t *testing.T) {
	dir := t.TempDir()
	release, err := lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock(dir); err == nil {
		t.Error("a second run took a conversation that the first still holds")
	}
	release()
	again, err := lock(dir)
	if err != nil {
		t.Fatalf("the conversation was not free after its run ended: %v", err)
	}
	again()
}
