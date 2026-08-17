package webui

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/agent"
	"github.com/ttak0422/track/internal/track/store"
)

// agentSession is the wire shape of one live agent session: the agent.Session record plus the
// project note and git branch resolved from its cwd when they can be found. Note and branch are
// deliberately omitted (never an error) when they cannot be resolved — a session running outside
// the vault's knowledge or outside git is an ordinary fact, not a failure.
type agentSession struct {
	agent.Session
	Note   *agentNote `json:"note,omitempty"`
	Branch string     `json:"branch,omitempty"`
}

// agentNote is the vault note a session's project resolves to, when one exists.
type agentNote struct {
	NoteID int64  `json:"note_id"`
	Title  string `json:"title"`
}

// handleAgents returns the live agent sessions, each decorated with the project note and git branch
// its cwd resolves to when they can be found. The vault is consulted only for that note — agent
// state is not a vault asset — so a nil store (New(cfg, nil)), a ResolveTerm failure, or a cwd
// that is not inside a git work tree simply skips the decoration and still answers 200.
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	sessions, err := agent.List(home)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	out := make([]agentSession, 0, len(sessions))
	for _, sess := range sessions {
		a := agentSession{Session: sess}
		if branch, ok := currentBranch(sess.CWD); ok {
			a.Branch = branch
		}
		if s.store != nil {
			if note, ok := projectNote(s.store, sess.CWD); ok {
				a.Note = &note
			}
		}
		out = append(out, a)
	}
	writeJSON(w, map[string]any{"sessions": out})
}

// currentBranch returns the branch a working directory is on, or ok=false when the directory is
// not inside a git work tree (or git cannot be run). A missing or non-repo cwd is ordinary — a
// session may run in a scratch dir — so it never becomes an error.
func currentBranch(dir string) (string, bool) {
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		return "", false
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return "", false
	}
	return branch, true
}

// projectNote resolves a session's cwd to the vault note named after its repository: the git work
// tree root's basename looked up as a note title, the same rule track-project-intake uses (and the
// same path `track resolve --term <name>` takes). A repo the vault has no note for, or a cwd that
// is not inside a repo, yields ok=false.
func projectNote(st *store.Store, dir string) (agentNote, bool) {
	root, ok := projectRoot(dir)
	if !ok {
		return agentNote{}, false
	}
	ref, found, err := st.ResolveTerm(filepath.Base(root))
	if err != nil || !found {
		return agentNote{}, false
	}
	return agentNote{NoteID: ref.NoteID, Title: ref.Title}, true
}

// projectRoot returns the git work tree root a working directory belongs to, or ok=false when it
// is not inside a repository.
func projectRoot(dir string) (string, bool) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false
	}
	return root, true
}

// handleAgentLog returns a session's transcript tail exactly as the agent package reads it — the
// Transcript is the whole response, not wrapped. id is the session id (required); tail defaults to
// 50 and must be positive, the CLI's tail>0 rule. A session whose transcript cannot be found is a
// 404. The vault is never consulted.
func (s *Server) handleAgentLog(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, errors.New("id is required"), http.StatusBadRequest)
		return
	}
	tail := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("tail")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, errors.New("tail must be a positive integer"), http.StatusBadRequest)
			return
		}
		tail = n
	}
	tr, err := agent.Log(home, id, tail)
	if err != nil {
		writeError(w, err, http.StatusNotFound)
		return
	}
	writeJSON(w, tr)
}