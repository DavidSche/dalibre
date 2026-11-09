package app

import (
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"dalibre/cmd/dalibre/locales"
	"dalibre/internal/app/icons"
	"dalibre/internal/app/theme"
	"dalibre/internal/app/widgets"
	"dalibre/internal/plugin"
)

// The Plugins dialog (Figma "06 · Plugins" and "07 · Get books"): the
// plugins Dalibre found, each one's switch, settings and access, and
// installing more.

type pluginsState struct {
	sel       string            // the plugin shown
	values    map[string]string // its settings as being edited
	valuesFor string            // whose values are being edited
	uninstall bool              // asking to uninstall sel
}

// openPlugins opens Settings at Plugins, on plugin id or the first.
func (a *App) openPlugins(id string) {
	a.openSettings(sectionPlugins)
	if id != "" {
		a.pl.sel = id
	}
}

// pluginStatus says what state a plugin is in, as its row shows it.
func (a *App) pluginStatus(k knownPlugin) (text string, tone ui.Color, loaded bool) {
	id := k.manifest.ID
	if p := a.pluginByID(id); p != nil {
		if p.MissingSettings() != nil {
			return locales.PluginsStatusNeeds(strings.Join(p.MissingSettings(), ", ")), pal().Warning.FG, true
		}
		v := k.manifest.Version
		switch {
		case k.builtin:
			v = locales.PluginsStatusBuiltIn(v)
		case k.linked != "":
			v = locales.PluginsStatusFromFolder(v)
		case k.manifest.Author != "":
			v = locales.PluginsStatusVersionBy(v, k.manifest.Author)
		}
		return v, pal().TextTertiary, true
	}
	if slices.ContainsFunc(a.pluginsPending, func(m *plugin.Manifest) bool { return m.ID == id }) {
		return locales.PluginsStatusNeedsReview(), pal().Warning.FG, false
	}
	if u := a.updates[id]; u != nil && loadedOK(a, id) {
		return locales.PluginsStatusUpdate(k.manifest.Version, u.label), pal().TextAccent, true
	}
	if st := a.settings.Plugins[id]; st != nil && st.Disabled {
		return locales.PluginsStatusOff(k.manifest.Version), pal().TextTertiary, false
	}
	return locales.PluginsStatusFailed(), pal().Danger.FG, false
}

func loadedOK(a *App, id string) bool { return a.pluginByID(id) != nil }

// pluginsPane is Settings › Plugins: the plugins Dalibre found, and the
// one chosen.
func (a *App) pluginsPane(c *ui.Context) {
	known := a.pluginsKnown
	if a.pl.sel == "" || !slices.ContainsFunc(known, func(k knownPlugin) bool { return k.manifest.ID == a.pl.sel }) {
		a.pl.sel = ""
		if len(known) > 0 {
			a.pl.sel = known[0].manifest.ID
		}
	}
	ui.Column(c).Width(248).Padding(theme.Space8).Gap(theme.Space2).Background(pal().Chrome).BorderWidth(0, 1, 0, 0).BorderColor(pal().BorderSubtle).AlignItems(ui.Stretch).Children(func() {
		ui.Scroll(c).Grow(1).MinHeight(0).Gap(theme.Space2).AlignItems(ui.Stretch).Children(func() {
			for _, k := range known {
				a.pluginRow(c.Key(k.manifest.ID), k)
			}
			for i, err := range a.pluginErrs {
				ui.Row(c.Key(i)).Padding(theme.Space8, theme.Space10).Gap(theme.Space8).AlignItems(ui.Start).Children(func() {
					ui.Icon(c, icons.Alert).FontSize(14).TextColor(pal().Danger.FG)
					ui.Text(c, err.Error()).FontSize(12).TextColor(pal().Danger.FG).MaxLines(3).Grow(1)
				})
			}
		})
		a.pluginsFolderActions(c)
	})
	ui.Scroll(c).Grow(1).MinWidth(0).Padding(theme.Space20).Gap(theme.Space16).AlignItems(ui.Stretch).Children(func() {
		for _, k := range known {
			if k.manifest.ID == a.pl.sel {
				a.pluginPane(c, k)
			}
		}
		if len(known) == 0 {
			ui.Text(c, locales.PluginsEmpty()).TextColor(pal().TextSecondary)
		}
	})
	if a.pl.uninstall {
		name := a.pluginName(a.pl.sel)
		title, msg, ok := locales.PluginsUninstallTitle(name), locales.PluginsUninstallMessage(), locales.PluginsUninstallConfirm()
		if st := a.settings.Plugins[a.pl.sel]; st != nil && st.Path != "" {
			title, msg, ok = locales.PluginsRemoveTitle(name), locales.PluginsRemoveMessage(), locales.CommonRemove()
		}
		if ui.AlertDialog(c, &a.pl.uninstall, title, msg, locales.CommonCancel(), ok) == 1 {
			a.uninstallPlugin(a.pl.sel)
		}
	}
}

// pluginRow is the PluginRow component.
func (a *App) pluginRow(c *ui.Context, k knownPlugin) {
	m := k.manifest
	sel := a.pl.sel == m.ID
	status, tone, loaded := a.pluginStatus(k)
	pending := status == locales.PluginsStatusNeedsReview()
	row := ui.ButtonBase(c).Padding(theme.Space8, theme.Space10).Gap(theme.Space10).Radius(theme.RadiusMD).AlignItems(ui.Center).Label(m.Name)
	switch {
	case sel:
		row.Background(pal().SurfaceRaised).Shadow(0, 1, 2, 0, ui.RGBA(26, 23, 18, 0.06)).Shadow(0, 0, 0, 1, pal().BorderSubtle.Alpha(0.6))
	case row.Hovered():
		row.Background(pal().Hover)
	}
	row.OnClick(func() { a.pl.sel = m.ID }).Children(func() {
		pluginTile(c, m, 28)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, m.Name).FontSize(13).FontWeight(500).SingleLine()
			ui.Text(c, status).FontSize(12).TextColor(tone).SingleLine()
		})
		switch {
		case pending:
			if widgets.ButtonSized(c, widgets.Secondary, nil, locales.PluginsReview(), theme.ControlS).Clicked() {
				a.askConsent(m.ID)
			}
		case a.updates[m.ID] != nil && loaded:
			if a.updates[m.ID].busy {
				ui.Spinner(c).Size(16, 16).Label(locales.PluginsUpdatingLabel(m.Name))
			} else if widgets.ButtonSized(c, widgets.Secondary, nil, locales.CommonUpdate(), theme.ControlS).Clicked() {
				a.updatePlugin(m.ID, false)
			}
		case status == locales.PluginsStatusFailed():
			ui.Icon(c, icons.Alert).FontSize(16).TextColor(pal().Danger.FG)
		default:
			on := loaded
			ui.Switch(c, &on).Label(locales.PluginsTurnOnLabel(m.Name))
			if on != loaded {
				a.setPluginEnabled(m.ID, on)
			}
		}
	})
}

// pluginTile is a plugin's icon on a tinted square.
func pluginTile(c *ui.Context, m *plugin.Manifest, size float32) {
	ui.Box(c).Size(size, size).Radius(theme.RadiusSM).Background(pal().AccentSubtle).Center().Children(func() {
		ui.Icon(c, pluginIcon(m)).FontSize(size * 0.55).TextColor(pal().TextAccent)
	})
}

// pluginsFolderActions ends the plugin list: Add plugin, a menu of the
// ways to install one, and ⋯ for the plugins folder.
func (a *App) pluginsFolderActions(c *ui.Context) {
	ui.Row(c).Padding(theme.Space8, 0, 0, 0).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
		add := ui.ButtonBase(c).Grow(1).Height(theme.ControlM).Padding(0, theme.Space12).Gap(theme.Space6).Radius(theme.RadiusSM).
			AlignItems(ui.Center).Justify(ui.Center).Background(pal().Surface).Border(1, pal().Border).Label(locales.PluginsAdd())
		if add.Hovered() {
			add.Border(1, pal().BorderStrong)
		}
		add.PopoverMenuButton(func(m *ui.Menu) {
			if a.pluginDir != "" && m.Item(locales.PluginsAddFromGithub()).Icon(icons.Menu(icons.GitBranch)).Chosen() {
				a.openGitHubInstall()
			}
			if m.Item(locales.PluginsAddFromFile()).Icon(icons.Menu(icons.File)).Chosen() {
				a.choosePluginPackage()
			}
			if m.Item(locales.PluginsAddFromFolder()).Icon(icons.Menu(icons.Folder)).Chosen() {
				a.choosePluginFolder()
			}
		})
		add.Children(func() {
			ui.Icon(c, icons.Plus).FontSize(14).TextColor(pal().Icon)
			ui.Text(c, locales.PluginsAdd()).FontSize(13).FontWeight(500)
		})
		if a.pluginDir == "" {
			return
		}
		more := ui.ButtonBase(c).Size(theme.ControlM, theme.ControlM).Radius(theme.RadiusSM).Center().Label(locales.PluginsMoreActions()).Tooltip(locales.PluginsMore())
		if more.Hovered() {
			more.Background(pal().Hover)
		}
		more.PopoverMenuButton(func(m *ui.Menu) {
			if m.Item(locales.PluginsOpenFolder()).Icon(icons.Menu(icons.Folder)).Chosen() {
				os.MkdirAll(a.pluginDir, 0o755)
				go mygo.Shell.OpenPath(a.pluginDir)
			}
			if m.Item(locales.PluginsReload()).Icon(icons.Menu(icons.Refresh)).Chosen() {
				a.loadPlugins()
			}
		})
		more.Children(func() { ui.Icon(c, icons.More).FontSize(16).TextColor(pal().Icon) })
	})
}

// choosePluginPackage asks for a .dalibre-plugin file and installs it.
func (a *App) choosePluginPackage() {
	a.run(func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: locales.PluginsInstallDialogTitle(),
			Filters: []mygo.FileFilter{{Name: locales.PluginsPackageFilter(), Extensions: []string{strings.TrimPrefix(plugin.PackageExt, ".")}}}})
		if err != nil || len(paths) == 0 {
			return
		}
		a.post(func() { a.installPlugin(paths[0]) })
	})
}

// choosePluginFolder asks for a plugin's folder (one with plugin.json)
// and links it: the plugin loads from there, and reloads as it changes.
func (a *App) choosePluginFolder() {
	a.run(func() {
		dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: locales.PluginsInstallFromFolderTitle(), Directory: true})
		if err != nil || len(dirs) == 0 {
			return
		}
		a.post(func() { a.installPlugin(dirs[0]) })
	})
}

// pluginPane is the plugin chosen: what it is, adds and may reach, and
// its settings.
func (a *App) pluginPane(c *ui.Context, k knownPlugin) {
	m := k.manifest
	_, _, loaded := a.pluginStatus(k)
	ui.Row(c).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
		pluginTile(c, m, 40)
		ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space2).Children(func() {
			ui.Text(c, m.Name).FontSize(16).FontWeight(600)
			parts := []string{locales.PluginsVersion(m.Version)}
			if m.Author != "" {
				parts = append(parts, locales.PluginsByAuthor(m.Author))
			}
			if m.License != "" {
				parts = append(parts, m.License)
			}
			ui.Text(c, strings.Join(parts, " · ")).FontSize(12).TextColor(pal().TextTertiary)
		})
	})
	if m.Description != "" {
		ui.Text(c, m.Description).FontSize(13).TextColor(pal().TextSecondary)
	}
	if u := a.updates[m.ID]; u != nil && loaded {
		a.updateBanner(c, m, u)
	}
	if st := a.settings.Plugins[m.ID]; st != nil && st.Source != nil && !k.builtin {
		a.pluginSource(c, m, st.Source)
	}
	ui.Column(c).Gap(theme.Space8).AlignItems(ui.Stretch).Children(func() {
		widgets.Overline(c, locales.PluginsAddsToTitle())
		ui.Row(c).Gap(theme.Space6).Children(func() {
			if m.IsBookSource() {
				widgets.Tag(c, locales.CommonGetBooks(), false, nil)
			}
			if m.IsMetadataSource() {
				widgets.Tag(c, locales.CommonFetchMetadata(), false, nil)
			}
		})
		if m.IsBookSource() {
			ui.Text(c, locales.PluginsGetBooksNote(m.Name)).FontSize(12).TextColor(pal().TextTertiary)
		}
		if m.IsMetadataSource() {
			ui.Text(c, locales.PluginsFetchMetadataNote(m.Name)).FontSize(12).TextColor(pal().TextTertiary)
		}
	})
	if len(m.Settings) > 0 {
		a.pluginSettingsForm(c, m, loaded)
	}
	ui.Column(c).Gap(theme.Space8).AlignItems(ui.Stretch).Children(func() {
		widgets.Overline(c, locales.PluginsCanAccess())
		for _, h := range m.Permissions.Hosts {
			permissionItem(c.Key(h), icons.Globe, h, "", false)
		}
		if m.Permissions.LocalNetwork {
			permissionItem(c, icons.Network, locales.PluginsLocalNetwork(), locales.PluginsLocalNetworkDetail(), true)
		}
		if m.Permissions.Library == plugin.LibraryRead {
			permissionItem(c, icons.Library, locales.PluginsLibraryRead(), locales.PluginsLibraryReadDetail(), false)
		}
		note := locales.PluginsNoteAllowed()
		if k.builtin {
			note = locales.PluginsNoteBuiltIn()
		} else if k.linked != "" {
			note = locales.PluginsNoteLinked(k.linked)
		}
		ui.Text(c, note).FontSize(12).TextColor(pal().TextTertiary)
	})
	ui.Row(c).Gap(theme.Space8).AlignItems(ui.Center).Children(func() {
		if k.linked != "" {
			if widgets.Button(c, widgets.Ghost, icons.Folder, locales.PluginsShowFolder()).Tooltip(k.linked).Clicked() {
				go mygo.Shell.OpenPath(k.linked)
			}
		} else if st := a.settings.Plugins[m.ID]; st != nil && st.Source != nil && !k.builtin {
			if widgets.Button(c, widgets.Ghost, icons.External, locales.PluginsViewOnGithub()).Clicked() {
				openURL("https://github.com/" + st.Source.Repo)
			}
		} else if m.Homepage != "" {
			if widgets.Button(c, widgets.Ghost, icons.External, locales.PluginsHomepage()).Tooltip(m.Homepage).Clicked() {
				openURL(m.Homepage)
			}
		}
		ui.Spacer(c)
		if !k.builtin {
			label := locales.PluginsUninstallButton()
			if k.linked != "" {
				label = locales.PluginsRemoveButton()
			}
			if widgets.Button(c, widgets.Secondary, nil, label).Clicked() {
				a.pl.uninstall = true
			}
		}
	})
}

// permissionItem is the PermissionItem component.
func permissionItem(c *ui.Context, icon *ui.SVG, label, detail string, warn bool) {
	bg, fg := pal().Subtle, pal().Text
	if warn {
		bg, fg = pal().Warning.BG, pal().Warning.FG
	}
	ui.Row(c).Padding(theme.Space8, theme.Space10).Gap(theme.Space10).Radius(theme.RadiusMD).Background(bg).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(fg)
		ui.Column(c).Grow(1).Children(func() {
			ui.Text(c, label).FontSize(13).TextColor(fg)
			if detail != "" {
				ui.Text(c, detail).FontSize(12).TextColor(fg.Alpha(0.8))
			}
		})
	})
}

// pluginSettingsForm edits a plugin's settings; Save reloads it with them.
func (a *App) pluginSettingsForm(c *ui.Context, m *plugin.Manifest, loaded bool) {
	saved := map[string]string{}
	if st := a.settings.Plugins[m.ID]; st != nil {
		maps.Copy(saved, st.Values)
	}
	maps.Copy(saved, a.pluginSecrets[m.ID])
	if a.pl.valuesFor != m.ID {
		a.pl.valuesFor, a.pl.values = m.ID, maps.Clone(saved)
		if a.pl.values == nil {
			a.pl.values = map[string]string{}
		}
	}
	ui.Column(c).Gap(theme.Space12).AlignItems(ui.Stretch).Children(func() {
		widgets.Overline(c, locales.PluginsSettingsTitle())
		for _, s := range m.Settings {
			v := a.pl.values[s.Key]
			hint := ""
			if s.Required {
				hint = locales.PluginsRequired()
			}
			ui.Column(c.Key(s.Key)).Gap(theme.Space6).AlignItems(ui.Stretch).Children(func() {
				ui.Row(c).Children(func() {
					ui.Text(c, s.Title).FontSize(12).FontWeight(500).TextColor(pal().TextSecondary).Grow(1)
					if hint != "" {
						ui.Text(c, hint).FontSize(12).TextColor(pal().TextTertiary)
					}
				})
				in := ui.TextInput(c, &v).Label(s.Title).Placeholder(s.Default)
				if s.Type == "secret" {
					in.Password()
				}
				if s.Description != "" {
					ui.Text(c, s.Description).FontSize(12).TextColor(pal().TextTertiary)
				}
			})
			a.pl.values[s.Key] = v
		}
		changed := !maps.Equal(trimmed(a.pl.values), trimmed(saved))
		ui.Row(c).Gap(theme.Space8).Children(func() {
			ui.Spacer(c)
			if widgets.Button(c, widgets.Primary, nil, locales.PluginsSaveSettings()).Disabled(!changed).Clicked() {
				a.setPluginValues(m.ID, trimmed(a.pl.values))
				a.toast("%s", locales.PluginsSavedSettings(m.Name))
			}
		})
	})
}

func trimmed(values map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out[k] = v
		}
	}
	return out
}

// consentDialog asks the user to let a plugin reach what it asks for.
func (a *App) consentDialog(c *ui.Context) {
	ask := a.consent
	if ask == nil {
		return
	}
	open := true
	m := ask.manifest
	// For an update, only what is new is asked.
	var had *plugin.Permissions
	if st := a.settings.Plugins[m.ID]; st != nil && st.Granted != nil {
		had = &plugin.Permissions{Hosts: st.Granted.Hosts, LocalNetwork: st.Granted.LocalNetwork, Library: st.Granted.Library}
	}
	title := locales.PluginsConsentInstallTitle(m.Name)
	if had != nil {
		title = locales.PluginsConsentAllowTitle(m.Name, m.Version)
	}
	widgets.DialogFrame(c, &open, 500, func() {
		ui.Text(c, title).FontSize(16).FontWeight(600).Grow(1)
	}, func() {
		ui.Column(c).Padding(theme.Space24).Gap(theme.Space16).AlignItems(ui.Stretch).Children(func() {
			ui.Row(c).Gap(theme.Space12).AlignItems(ui.Center).Children(func() {
				pluginTile(c, m, 40)
				ui.Column(c).Grow(1).Children(func() {
					ui.Text(c, m.Name).FontSize(14).FontWeight(600)
					by := locales.PluginsVersion(m.Version)
					if m.Author != "" {
						by = locales.PluginsConsentVersionBy(m.Version, m.Author)
					}
					ui.Text(c, by).FontSize(12).TextColor(pal().TextTertiary)
				})
			})
			if m.Description != "" {
				ui.Text(c, m.Description).FontSize(13).TextColor(pal().TextSecondary)
			}
			overline := locales.PluginsConsentWillBeAble()
			if had != nil {
				overline = locales.PluginsConsentAsksMore()
			}
			widgets.Overline(c, overline)
			for _, h := range m.Permissions.Hosts {
				if had != nil && slices.Contains(had.Hosts, h) {
					continue
				}
				permissionItem(c.Key(h), icons.Globe, locales.PluginsConsentConnect(h), "", false)
			}
			if m.Permissions.LocalNetwork && (had == nil || !had.LocalNetwork) {
				permissionItem(c, icons.Network, locales.PluginsConsentLocal(),
					locales.PluginsConsentLocalDetail(), true)
			}
			if m.Permissions.Library == plugin.LibraryRead && (had == nil || had.Library != plugin.LibraryRead) {
				permissionItem(c, icons.Library, locales.PluginsConsentSeeBooks(), locales.PluginsLibraryReadDetail(), false)
			}
			if had != nil && len(had.Hosts) > 0 {
				ui.Text(c, locales.PluginsConsentAlreadyAllowed(strings.Join(had.Hosts, ", "))).FontSize(12).TextColor(pal().TextTertiary)
			}
			sandbox := locales.PluginsConsentSandbox()
			if m.Permissions.Library != "" {
				sandbox = locales.PluginsConsentSandboxLibrary()
			}
			ui.Text(c, sandbox).FontSize(12).TextColor(pal().TextTertiary)
			if m.Homepage != "" {
				ui.Text(c, m.Homepage).FontSize(12).TextColor(pal().TextTertiary).Selectable()
			}
		})
	}, func() {
		ui.Spacer(c)
		if widgets.Button(c, widgets.Secondary, nil, locales.CommonCancel()).Clicked() {
			open = false
		}
		label := locales.PluginsConsentInstall()
		if had != nil || ask.archive == "" {
			label = locales.PluginsConsentAllow()
		}
		if widgets.Button(c, widgets.Primary, nil, label).Clicked() {
			a.grantConsent()
		}
	})
	if !open {
		a.consent = nil
	}
}

// libraryReadDetail: see locales.PluginsLibraryReadDetail.
