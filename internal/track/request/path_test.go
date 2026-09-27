package request

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRejectsEscapingIDs(t *testing.T) {
	st, _ := newTestStore(t)
	created, err := st.Create(NewRequest{Intent: IntentExplain, Instruction: "explain", AgentID: "agent"}, at(0))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(st.dir, created.Request.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(st.dir, "..", "outside.json")
	if err := os.WriteFile(outside, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../outside", "nested/../../outside", `..\outside`} {
		t.Run(id, func(t *testing.T) {
			if _, err := st.Get(id); err == nil {
				t.Error("read accepted escaping ID")
			}
			r := created.Request
			r.ID = id
			if err := st.save(&r, at(0)); err == nil {
				t.Error("write accepted escaping ID")
			}
		})
	}
}
