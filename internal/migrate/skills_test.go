package migrate

import (
	"context"
	"testing"

	"github.com/ossmalaysia/claude-sync/internal/claudeapi"
	"github.com/ossmalaysia/claude-sync/internal/store"
)

func skillSource() *fakeAPI {
	api := newFakeAPI()
	api.addSkill("docs", "anthropic-example", "anthropic", "t", []byte("builtin"))
	api.addSkill("quotation", "custom", "user", "t1", []byte("PK-quotation"))
	api.addSkill("invoice", "plugin", "user", "t1", []byte("PK-invoice-with-assets"))
	api.addSkill("sql", "plugin", "anthropic", "t", []byte("anthropic-plugin"))
	return api
}

func TestPullSavesOnlyPersonalSkillsWithTheirPackage(t *testing.T) {
	api := skillSource()
	st := newTestStore(t)
	res, err := Pull(context.Background(), api, st, "o", testOpts(), nil)
	if err != nil || res.Skills != 2 || res.SkillsRead != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	skills, _ := st.ListSkills()
	if len(skills) != 2 || skills[0].Name != "invoice" || skills[1].Name != "quotation" {
		t.Fatalf("skills=%+v", skills)
	}
	if pkg, _ := st.ReadSkill(skills[0].ID); string(pkg) != "PK-invoice-with-assets" {
		t.Fatalf("package=%q", pkg)
	}
	// Unchanged skills are not downloaded again; a changed one is.
	api.skills[1].UpdatedAt = "t2"
	res, err = Pull(context.Background(), api, st, "o", testOpts(), nil)
	if err != nil || res.SkillsRead != 1 || api.count("DownloadSkill") != 3 {
		t.Fatalf("delta res=%+v err=%v downloads=%d", res, err, api.count("DownloadSkill"))
	}
}

func TestPushUploadsSkillsOnceAndAdoptsSameName(t *testing.T) {
	st := newTestStore(t)
	st.SaveSkill(store.SkillMeta{ID: "skill_q", Name: "quotation", UpdatedAt: "t1"}, []byte("PK-q"))
	st.SaveSkill(store.SkillMeta{ID: "skill_i", Name: "invoice", UpdatedAt: "t1"}, []byte("PK-i"))
	target := newFakeAPI()
	target.addSkill("invoice", "custom", "user", "t", []byte("already there")) // e.g. uploaded by hand
	state, _ := st.LoadState()

	res, err := Push(context.Background(), target, st, state, "org", map[string]bool{}, testOpts(), nil)
	if err != nil || res.Skills != 1 || target.count("UploadSkill") != 1 {
		t.Fatalf("res=%+v err=%v uploads=%d", res, err, target.count("UploadSkill"))
	}
	if target.skillPkg[target.skills[len(target.skills)-1].ID] == nil || string(target.skillPkg[target.skills[len(target.skills)-1].ID]) != "PK-q" {
		t.Fatal("uploaded package differs from the pulled one")
	}
	if state.Skills["skill_i"].Status != store.StatusDone || state.Skills["skill_q"].Status != store.StatusDone {
		t.Fatalf("state=%+v", state.Skills)
	}
	if res, _ := Push(context.Background(), target, st, state, "org", map[string]bool{}, testOpts(), nil); res.Skills != 0 || target.count("UploadSkill") != 1 {
		t.Fatalf("second push uploaded again: %+v", res)
	}
}

// An upload that took effect but whose reply was lost must not be retried
// into claude.ai's "already have a skill named" error.
func TestPushSkillRetryAdoptsUploadWhoseReplyWasLost(t *testing.T) {
	st := newTestStore(t)
	st.SaveSkill(store.SkillMeta{ID: "skill_q", Name: "quotation", UpdatedAt: "t1"}, []byte("PK-q"))
	target := newFakeAPI()
	target.lostReply = httpErr(503, claudeapi.ErrServer)
	state, _ := st.LoadState()

	res, err := Push(context.Background(), target, st, state, "org", map[string]bool{}, testOpts(), nil)
	if err != nil || len(res.Failed) != 0 || res.Skills != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if n := target.count("UploadSkill"); n != 1 {
		t.Fatalf("uploads=%d, want 1", n)
	}
	if state.Skills["skill_q"].Status != store.StatusDone {
		t.Fatalf("state=%+v", state.Skills["skill_q"])
	}
}

func TestPlanCountsNewSkills(t *testing.T) {
	st := newTestStore(t)
	st.SaveSkill(store.SkillMeta{ID: "skill_q", Name: "quotation"}, []byte("PK"))
	st.SaveSkill(store.SkillMeta{ID: "skill_i", Name: "invoice"}, []byte("PK"))
	state := &store.State{Skills: map[string]*store.ItemState{"skill_i": {Status: store.StatusDone}}}
	res, err := Plan(st, map[string]bool{}, state, "org", testOpts())
	if err != nil || res.NewSkills != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// Accounts without the skills feature answer 404; that must not fail a pull.
func TestPullWithoutSkillsFeature(t *testing.T) {
	api := newFakeAPI()
	api.addProject("A", "", nil, nil)
	api.failNext("ListSkills", notFound())
	res, err := Pull(context.Background(), api, newTestStore(t), "o", testOpts(), nil)
	if err != nil || res.Skills != 0 || res.Projects != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
