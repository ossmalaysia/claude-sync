package store

import "testing"

func TestSkillRoundTrip(t *testing.T) {
	s := open(t)
	if _, ok, err := s.LoadSkill("skill_01A"); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if err := s.SaveSkill(SkillMeta{ID: "skill_01A", Name: "quotation", UpdatedAt: "t1"}, []byte("PK")); err != nil {
		t.Fatal(err)
	}
	m, ok, err := s.LoadSkill("skill_01A")
	if !ok || err != nil || m.Name != "quotation" {
		t.Fatalf("m=%+v ok=%v err=%v", m, ok, err)
	}
	if b, _ := s.ReadSkill("skill_01A"); string(b) != "PK" {
		t.Fatalf("pkg=%q", b)
	}
	if all, _ := s.ListSkills(); len(all) != 1 {
		t.Fatalf("all=%+v", all)
	}
	if err := s.SaveSkill(SkillMeta{ID: "../x"}, nil); err == nil {
		t.Fatal("unsafe id accepted")
	}
}
