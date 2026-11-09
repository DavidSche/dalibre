package app

import (
	"fmt"
	"net/url"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"dalibre/cmd/dalibre/locales"
	"dalibre/internal/app/icons"
	"dalibre/internal/app/theme"
)

// Feedback (Figma "12 · Feedback"): a sidebar row above Settings whose
// menu opens a new GitHub issue, a bug report or a feature request, or the
// list of issues.

const issuesURL = "https://github.com/DavidSche/dalibre/issues"

// bugReportURL opens a new issue with the version and system filled in.
func bugReportURL(version string) string {
	body := fmt.Sprintf("**What happened?**\n\n\n**Steps to reproduce**\n\n1. \n\n**What did you expect?**\n\n\n---\nDalibre %s · %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
	return issuesURL + "/new?" + url.Values{"labels": {"bug"}, "body": {body}}.Encode()
}

// featureRequestURL opens a new issue for an idea.
func featureRequestURL() string {
	body := "**What would you like Dalibre to do?**\n\n\n**Why would it help you?**\n\n"
	return issuesURL + "/new?" + url.Values{"labels": {"enhancement"}, "body": {body}}.Encode()
}

func (a *App) feedbackNav(c *ui.Context) {
	row := ui.ButtonBase(c.Key("feedback")).Height(30).Radius(theme.RadiusSM).Padding(0, theme.Space8).Gap(theme.Space8).AlignItems(ui.Center).Label(locales.MainFeedback())
	if a.feedbackOpen || row.Hovered() {
		row.Background(pal().Hover)
	}
	// The menu opens above the row, its start lined up with the row's.
	row.PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(locales.MainFeedbackReportBug()).Icon(icons.Menu(icons.Bug)).Chosen() {
			openURL(bugReportURL(mygo.App.Version()))
		}
		if m.Item(locales.MainFeedbackRequestFeature()).Icon(icons.Menu(icons.Sparkle)).Chosen() {
			openURL(featureRequestURL())
		}
		m.Separator()
		if m.Item(locales.MainFeedbackViewIssues()).Icon(icons.Menu(icons.External)).Chosen() {
			openURL(issuesURL)
		}
	}, ui.PopoverMenuButtonOptions{At: ui.AnchorTopStart, Self: ui.AnchorBottomStart, Open: &a.feedbackOpen})
	row.Children(func() {
		ui.Icon(c, icons.MessageSquare).FontSize(16).TextColor(pal().Icon)
		ui.Text(c, locales.MainFeedback()).Grow(1).SingleLine()
	})
}
