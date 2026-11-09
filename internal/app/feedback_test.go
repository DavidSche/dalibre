package app

import (
	"net/url"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestFeedback opens the sidebar's Feedback menu and checks the issues it
// links to.
func TestFeedback(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if err := tt.Click("Feedback"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, s := range []string{"Report a bug…", "Request a feature…", "View issues on GitHub"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	if !a.feedbackOpen {
		t.Error("feedbackOpen is false with the menu showing")
	}

	u, err := url.Parse(bugReportURL("1.2.3"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/DavidSche/dalibre/issues/new" || u.Query().Get("labels") != "bug" {
		t.Errorf("bug report URL %s", u)
	}
	if body := u.Query().Get("body"); !strings.Contains(body, "Dalibre 1.2.3 · ") {
		t.Errorf("bug report body has no version: %q", body)
	}
	if u, _ := url.Parse(featureRequestURL()); u.Query().Get("labels") != "enhancement" {
		t.Errorf("feature request URL %s", u)
	}
}
