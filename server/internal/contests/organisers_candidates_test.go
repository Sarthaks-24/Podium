package contests

import (
	"context"
	"testing"
)

func TestOrganiserCandidates(t *testing.T) {
	r := newSvc(t)
	c := r.contest(t, nil)
	ctx := context.Background()
	if _, err := r.svc.DB.Exec(`INSERT INTO users(id,email,name,role,disabled,created_at) VALUES
		(4,'Zed@college.edu','Zed','organiser',0,0),
		(5,'amy@college.edu','','admin',0,0),
		(6,'off@college.edu','Off','organiser',1,0),
		(7,'listed@college.edu','Listed','organiser',0,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.DB.Exec(`INSERT INTO contest_organisers(contest_id,user_id,added_by,added_at) VALUES(?,?,?,?)`, c.ID, 7, 1, 1); err != nil {
		t.Fatal(err)
	}
	got, err := r.svc.OrganiserCandidates(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The creator is already on the contest, students and disabled people
	// are never listed, and the order ignores case.
	if len(got) != 2 || got[0].Email != "amy@college.edu" || got[0].Role != "admin" || got[1].Email != "Zed@college.edu" {
		t.Fatalf("candidates = %#v", got)
	}
	if _, err := r.svc.DB.Exec(`DELETE FROM contest_organisers WHERE contest_id=? AND user_id=?`, c.ID, 7); err != nil {
		t.Fatal(err)
	}
	got, err = r.svc.OrganiserCandidates(ctx, c.ID)
	if err != nil || len(got) < 3 {
		t.Fatalf("after removal = %#v, %v", got, err)
	}
}
