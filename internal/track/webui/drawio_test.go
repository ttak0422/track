package webui

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ttak0422/track/internal/track/export"
)

func TestDrawioFenceMapping(t *testing.T) {
	xml := `<mxfile><diagram id="first">compressed</diagram><diagram id="second">payload</diagram></mxfile>`
	body := "before\n\n````drawio :name overview\n" + xml + "\n````\n\n```drawio\n" + xml + "\n```\nafter\n"
	marked, fences := markDrawioFences(body)
	if len(fences) != 2 {
		t.Fatalf("fences = %#v", fences)
	}
	for i, line := range []int{2, 6} {
		if fences[i].StartLine != line || fences[i].EndLine != line+2 || fences[i].XML != xml {
			t.Fatalf("wrong mapping: %#v", fences[i])
		}
	}
	if fences[0].ID == fences[1].ID {
		t.Fatal("duplicate identities")
	}
	rendered, err := export.WebBody(marked)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "````drawio :name overview "+fences[0].ID) || !strings.Contains(rendered, xml) {
		t.Fatal(rendered)
	}
	_, next := markDrawioFences(body)
	if next[0].ID == fences[0].ID {
		t.Fatal("marker reused between renders")
	}
}

func TestDrawioFenceMappingSkipsOtherBlocks(t *testing.T) {
	for _, body := range []string{
		"````markdown\n```drawio\n<mxfile />\n```\n````\n",
		"~~~markdown\n```drawio\n<mxfile />\n```\n~~~\n",
		"```drawio\n<mxfile />\n",
		"- item\n  ```drawio\n  <mxfile />\n  ```\n",
		"> ```drawio\n> <mxfile />\n> ```\n",
		"![diagram](assets/diagram.drawio)\n![[Other note]]\n",
	} {
		marked, fences := markDrawioFences(body)
		if len(fences) != 0 || marked != body {
			t.Fatalf("unexpected edit capability for %q: %#v", body, fences)
		}
	}
}

func TestRenderMapsDrawioBeforeExpansion(t *testing.T) {
	server, _ := putNoteSetup(t, 100, "Synthetic", "body\n")
	body := "```dashboard\njournal\n```\n\n```drawio\n<mxfile />\n```\n"
	data, _ := json.Marshal(map[string]string{"body": body})
	resp, err := http.Post(server.URL+"/api/render", "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result struct {
		Markdown string        `json:"markdown"`
		Fences   []drawioFence `json:"drawio_fences"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || len(result.Fences) != 1 {
		t.Fatalf("response: %d %#v", resp.StatusCode, result)
	}
	fence := result.Fences[0]
	if fence.StartLine != 4 || fence.EndLine != 6 || fence.XML != "<mxfile />" || !strings.Contains(result.Markdown, fence.ID) {
		t.Fatalf("response: %#v", result)
	}
}
