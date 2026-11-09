package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"dalibre/cmd/dalibre/locales"
	"dalibre/internal/app/icons"
	"dalibre/internal/app/theme"
	"dalibre/internal/app/widgets"
)

// updatesGroup is Settings › General's Updates: the version and its
// status, the check, and the two switches.
func (a *App) updatesGroup(c *ui.Context) {
	u := &a.au
	s := &a.settings.AppUpdates
	widgets.Hairline(c)
	ui.Column(c.Key("app-updates")).Gap(theme.Space12).AlignItems(ui.Stretch).Children(func() {
		ui.Row(c).Gap(theme.Space16).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space4).Children(func() {
				ui.Text(c, locales.AppUpdatesVersion(u.version)).FontSize(13).FontWeight(600)
				ui.Row(c).Gap(theme.Space6).AlignItems(ui.Center).Children(func() {
					a.updateStatus(c)
				})
			})
			switch u.status {
			case auIdle, auChecking:
				if widgets.Button(c, widgets.Secondary, nil, locales.AppUpdatesCheck()).Disabled(u.status == auChecking).Clicked() {
					a.checkAppUpdate(true)
				}
			case auCheckFailed:
				if widgets.Button(c, widgets.Secondary, nil, locales.AppUpdatesTryAgain()).Clicked() {
					a.checkAppUpdate(true)
				}
			}
		})
		a.updateBannerApp(c)
	})
	widgets.Hairline(c)
	ui.Row(c).Gap(theme.Space16).AlignItems(ui.Center).Children(func() {
		heading(c, locales.AppUpdatesAuto(), "")
		on := !s.Manual
		ui.Switch(c, &on).Label(locales.AppUpdatesAuto())
		if on == s.Manual {
			s.Manual = !on
			a.saveSettings()
			a.checkAppUpdate(false)
		}
	})
	widgets.Hairline(c)
	ui.Row(c).Gap(theme.Space16).AlignItems(ui.Center).Children(func() {
		heading(c, locales.AppUpdatesBeta(), "")
		on := s.Beta
		ui.Switch(c, &on).Label(locales.AppUpdatesBeta())
		if on != s.Beta {
			s.Beta = on
			a.saveSettings()
		}
	})
}

// updateStatus is the line under the version.
func (a *App) updateStatus(c *ui.Context) {
	u := &a.au
	checked := a.settings.AppUpdates.Checked
	switch u.status {
	case auChecking:
		ui.Spinner(c).Size(12, 12).Label(locales.AppUpdatesChecking())
		ui.Text(c, locales.AppUpdatesChecking()).FontSize(12).TextColor(pal().TextTertiary)
	case auCheckFailed:
		ui.Icon(c, icons.Alert).FontSize(13).TextColor(pal().Danger.FG)
		ui.Text(c, u.err).FontSize(12).TextColor(pal().Danger.FG)
	case auIdle:
		if checked.IsZero() {
			ui.Text(c, locales.AppUpdatesNotChecked()).FontSize(12).TextColor(pal().TextTertiary)
		} else {
			ui.Text(c, locales.AppUpdatesUpToDate(ago(checked))).FontSize(12).TextColor(pal().TextTertiary)
		}
	default:
		if !checked.IsZero() {
			ui.Text(c, locales.AppUpdatesChecked(ago(checked))).FontSize(12).TextColor(pal().TextTertiary)
		}
	}
}

// updateBannerApp is the banner of a new version, as the plugins' update
// banner: available, downloading, ready or failed.
func (a *App) updateBannerApp(c *ui.Context) {
	u := &a.au
	r := u.release
	if r == nil || u.status < auAvailable {
		return
	}
	bg, fg, icon := pal().AccentSubtle, pal().TextAccent, icons.ArrowUpCircle
	switch u.status {
	case auReady:
		icon = icons.CheckCircle
	case auInstallFailed:
		bg, fg, icon = pal().Danger.BG, pal().Danger.FG, icons.Alert
	}
	ui.Row(c).Padding(theme.Space12, theme.Space12).Gap(theme.Space12).Radius(theme.RadiusMD).Background(bg).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, icon).FontSize(16).TextColor(fg)
		ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space8).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Gap(theme.Space2).AlignItems(ui.Stretch).Children(func() {
				var title, body string
				switch u.status {
				case auAvailable:
					title = locales.AppUpdatesAvailable(r.Version)
					body = locales.AppUpdatesReleased(ago(r.Date))
					if n := releaseNotes(r.Notes, 2); len(n) > 0 {
						body += " " + strings.Join(n, " · ")
					}
				case auDownloading:
					title = locales.AppUpdatesDownloading(r.Version)
				case auReady:
					title = locales.AppUpdatesReady(r.Version)
					body = locales.AppUpdatesReadyBody()
				case auInstallFailed:
					title = locales.AppUpdatesInstallFailed(r.Version)
					body = u.err + " " + locales.AppUpdatesNothingChanged()
				}
				ui.Text(c, title).FontSize(13).FontWeight(500).TextColor(fg)
				if body != "" {
					ui.Text(c, body).FontSize(12).TextColor(pal().TextSecondary).MaxLines(3)
				}
			})
			if u.status == auDownloading {
				ui.Column(c).Gap(theme.Space6).AlignItems(ui.Stretch).Children(func() {
					ui.Progress(c, u.fraction()).Height(4)
					ui.Text(c, u.progressText()).FontSize(12).TextColor(pal().TextSecondary)
				})
			}
			ui.Row(c).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
				a.updateActions(c, theme.ControlS, false)
			})
		})
	})
}

// updateActions are the buttons for the update's status, in the banner
// or, when dialog, in the update dialog's footer. Later is the dialog's:
// the banner stays.
func (a *App) updateActions(c *ui.Context, size float32, dialog bool) {
	u := &a.au
	switch u.status {
	case auAvailable:
		if widgets.ButtonSized(c, widgets.Primary, nil, locales.AppUpdatesInstall(), size).Clicked() {
			a.installAppUpdate()
		}
		if !dialog && widgets.ButtonSized(c, widgets.Ghost, icons.External, locales.AppUpdatesWhatsNew(), size).Clicked() {
			openURL(releasesURL)
		}
	case auDownloading:
		if widgets.ButtonSized(c, ternary(dialog, widgets.Secondary, widgets.Ghost), nil, locales.CommonCancel(), size).Clicked() {
			a.cancelAppUpdate()
		}
	case auReady:
		if widgets.ButtonSized(c, ternary(dialog, widgets.Secondary, widgets.Primary), nil, ternary(dialog, locales.AppUpdatesLater(), locales.AppUpdatesRestart()), size).Clicked() {
			if dialog {
				u.dialog = false
			} else {
				u.relaunch()
			}
		}
		if dialog && widgets.ButtonSized(c, widgets.Primary, nil, locales.AppUpdatesRestart(), size).Clicked() {
			u.relaunch()
		}
	case auInstallFailed:
		if widgets.ButtonSized(c, widgets.Secondary, nil, locales.AppUpdatesTryAgain(), size).Clicked() {
			a.installAppUpdate()
		}
		if widgets.ButtonSized(c, widgets.Ghost, icons.External, locales.AppUpdatesDownloadFromGitHub(), size).Clicked() {
			openURL(releasesURL + "/latest")
		}
	}
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

func (u *appUpdateState) fraction() float64 {
	if u.total <= 0 {
		return -1
	}
	return float64(u.done) / float64(u.total)
}

// progressText is "14.6 of 36.5 MB", in decimal megabytes as Finder.
func (u *appUpdateState) progressText() string {
	if u.total <= 0 {
		return ""
	}
	mb := func(n int64) string { return fmt.Sprintf("%.1f", float64(n)/1e6) }
	return locales.AppUpdatesProgress(mb(u.done), mb(u.total))
}

// appUpdateDialog offers a version that a background check found.
func (a *App) appUpdateDialog(c *ui.Context) {
	u := &a.au
	r := u.release
	if !u.dialog || r == nil || u.status < auAvailable {
		return
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(pal().Scrim)
		panel.Width(480).MaxHeight(ui.Auto).Background(pal().SurfaceRaised).Border(1, pal().BorderSubtle).Radius(theme.RadiusXL).Clip().
			Shadow(0, 2, 6, 0, ui.RGBA(26, 23, 18, 0.08)).Shadow(0, 24, 48, -8, ui.RGBA(26, 23, 18, 0.24)).AlignItems(ui.Stretch)
		panel.Children(func() {
			ui.Row(c).Padding(theme.Space24, theme.Space24, theme.Space20, theme.Space24).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Width(48).Height(48).Radius(theme.RadiusMD).Background(pal().AccentSubtle).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					ui.Icon(c, icons.ArrowUpCircle).FontSize(24).TextColor(pal().TextAccent)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space2).Children(func() {
					ui.Text(c, locales.AppUpdatesAvailable(r.Version)).FontSize(18).FontWeight(600)
					date := locales.AppUpdatesYouHave(u.version)
					if !r.Date.IsZero() {
						date = locales.AppUpdatesYouHaveReleased(u.version, i18n.FormatDate(i18n.Locale(), r.Date, i18n.DateOptions{Skeleton: "yMMMd"}))
					}
					ui.Text(c, date).FontSize(12).TextColor(pal().TextTertiary)
				})
			})
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Padding(0, theme.Space24, theme.Space24, theme.Space24).Gap(theme.Space8).AlignItems(ui.Stretch).Children(func() {
					if notes := releaseNotes(r.Notes, 12); len(notes) > 0 {
						widgets.Overline(c, locales.AppUpdatesWhatsNewOverline())
						ui.Column(c).Padding(theme.Space12, theme.Space16).Gap(theme.Space6).Radius(theme.RadiusMD).Background(pal().Subtle).AlignItems(ui.Stretch).Children(func() {
							for i, n := range notes {
								ui.Row(c.Key(i)).Gap(theme.Space8).AlignItems(ui.Start).Children(func() {
									ui.Text(c, "•").FontSize(13)
									ui.Text(c, n).FontSize(13).Grow(1).MinWidth(0)
								})
							}
						})
					}
					ui.Row(c).Children(func() {
						if widgets.ButtonSized(c, widgets.Ghost, icons.External, locales.AppUpdatesFullNotes(), theme.ControlS).Clicked() {
							openURL(releasesURL)
						}
					})
				})
			})
			ui.Row(c).Padding(theme.Space16, theme.Space24).Gap(theme.Space8).AlignItems(ui.Center).Background(pal().Chrome).BorderWidth(1, 0, 0, 0).BorderColor(pal().BorderSubtle).Children(func() {
				switch u.status {
				case auAvailable:
					if widgets.Button(c, widgets.Ghost, nil, locales.AppUpdatesSkip()).Clicked() {
						a.skipAppUpdate()
					}
					ui.Spacer(c)
					if widgets.Button(c, widgets.Secondary, nil, locales.AppUpdatesLater()).Clicked() {
						u.dialog = false
					}
				case auDownloading:
					ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space6).AlignItems(ui.Stretch).Children(func() {
						ui.Progress(c, u.fraction()).Height(4)
						text := locales.AppUpdatesDownloading(r.Version)
						if p := u.progressText(); p != "" {
							text = locales.AppUpdatesDownloadingProgress(p)
						}
						ui.Text(c, text).FontSize(12).TextColor(pal().TextSecondary)
					})
					ui.Box(c).Width(theme.Space16)
				case auReady:
					ui.Text(c, locales.AppUpdatesReadyBody()).FontSize(12).TextColor(pal().TextSecondary).Grow(1).MinWidth(0)
				case auInstallFailed:
					ui.Text(c, u.err).FontSize(12).TextColor(pal().Danger.FG).Grow(1).MinWidth(0).MaxLines(2)
				}
				a.updateActions(c, theme.ControlM, true)
			})
		})
	})
	if !open {
		u.dialog = false
	}
}
