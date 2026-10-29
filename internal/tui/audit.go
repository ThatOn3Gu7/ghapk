package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Audit interactions and meaningful state transitions, but never record text
// typed into search, filters, repo editors, or settings fields.
func (m Model) auditInput(msg tea.Msg) {
	if m.activity == nil {
		return
	}
	switch v := msg.(type) {
	case tea.KeyMsg:
		k := v.String()
		textInput := m.filterMode || m.modal == ModalPickRepo || m.modal == ModalPickBranch || m.modal == ModalPickWorkflow || m.modal == ModalEditRepo || (m.focus == PanelSettings && m.settingsUI != nil && m.settingsUI.editing)
		if textInput && (len(v.Runes) > 0 || k == "space" || k == "backspace" || k == "delete") {
			return
		}
		if len(v.Runes) > 1 {
			k = "[pasted text omitted]"
		}
		_ = m.activity.Write("input", fmt.Sprintf("key %s (panel=%s, dialog=%d)", k, PanelNames[m.activeTab()], m.modal))
	case tea.MouseMsg:
		if v.Action == tea.MouseActionPress || v.Type == tea.MouseWheelUp || v.Type == tea.MouseWheelDown {
			_ = m.activity.Write("input", fmt.Sprintf("pointer %s at %d,%d (panel=%s)", v.String(), v.X, v.Y, PanelNames[m.activeTab()]))
		}
	case jobEventMsg:
		if v.transient {
			break
		}
		if v.log != "" {
			_ = m.activity.Write("job", fmt.Sprintf("job #%d: %s", v.id, v.log))
		} else if v.phase != "" {
			_ = m.activity.Write("job", fmt.Sprintf("job #%d: %s", v.id, v.phase))
		}
	case jobFinishedMsg:
		result := v.status
		if v.err != nil {
			result += "; " + v.err.Error()
		}
		_ = m.activity.Write("job", fmt.Sprintf("job #%d finished: %s", v.id, result))
	case jobQuestionMsg:
		_ = m.activity.Write("job", fmt.Sprintf("job #%d awaiting choice: %s", v.id, v.title))
	case FatalErrMsg:
		_ = m.activity.Write("error", fmt.Sprintf("%s: %v", v.Context, v.Err))
	case AuthCheckedMsg:
		m.auditResult("GitHub authentication", v.Err)
	case RepoInfoMsg:
		m.auditResult("repository metadata", v.Err)
	case WorkflowsMsg:
		m.auditResult(fmt.Sprintf("workflows loaded (%d)", len(v.List)), v.Err)
	case RunsMsg:
		m.auditResult(fmt.Sprintf("builds loaded (%d)", len(v.List)), v.Err)
	case ArtifactsMsg:
		m.auditResult(fmt.Sprintf("artifacts loaded (%d)", len(v.List)), v.Err)
	case BranchesMsg:
		m.auditResult(fmt.Sprintf("branches loaded (%d)", len(v.List)), v.Err)
	case ReleasesMsg:
		m.auditResult(fmt.Sprintf("releases loaded (%d)", len(v.List)), v.Err)
	case UserReposMsg:
		m.auditResult(fmt.Sprintf("repositories loaded (%d)", len(v.List)), v.Err)
	case WorkflowFileMsg:
		m.auditResult("workflow preview", v.Err)
	case DownloadDoneMsg:
		m.auditResult("download", v.Err)
	case InstallDoneMsg:
		m.auditResult("installer", v.Err)
	case DeviceMsg:
		_ = m.activity.Write("device", "Device probe completed")
	case deviceChecksMsg:
		_ = m.activity.Write("device", "Device diagnostics completed")
	case deviceHistoryMsg:
		_ = m.activity.Write("device", "Install history loaded")
	case SearchResultsMsg:
		m.auditResult(fmt.Sprintf("repository search completed (%d)", len(v.Hits)), v.Err)
	case PackageQueryMsg:
		m.auditResult("installed package query", v.Err)
	case planReadyMsg:
		_ = m.activity.Write("app", "Install preflight completed")
	case InstallPreparedMsg:
		_ = m.activity.Write("installer", "APK inspection completed")
	case InstallVerifyMsg:
		_ = m.activity.Write("installer", "Install verification: "+v.Result.Detail)
	case ZipInspectedMsg:
		m.auditResult("archive inspection", v.Err)
	case DoctorDoneMsg:
		_ = m.activity.Write("app", "Doctor checks completed")
	case RefreshTickMsg:
		if m.screen == ScreenDashboard && m.modal == ModalNone && m.settings.Repo != "" && v.Seq == m.refreshSeq {
			_ = m.activity.Write("app", "Automatic build refresh")
		}
	}
}
func (m Model) auditResult(what string, err error) {
	if err != nil {
		_ = m.activity.Write("error", what+": "+err.Error())
	} else {
		_ = m.activity.Write("app", what+" completed")
	}
}
func (m Model) auditTransition(next Model) {
	if m.activity == nil {
		return
	}
	if next.activeTab() != m.activeTab() {
		_ = m.activity.Write("navigation", "Opened "+PanelNames[next.activeTab()])
	}
	if next.settings.Repo != m.settings.Repo {
		_ = m.activity.Write("selection", "Repository changed to "+next.settings.Repo)
	}
	if next.branchSel != m.branchSel {
		_ = m.activity.Write("selection", "Branch changed")
	}
	if next.settings.Workflow != m.settings.Workflow {
		_ = m.activity.Write("selection", "Workflow changed")
	}
	if next.modal != m.modal {
		if next.modal == ModalNone {
			_ = m.activity.Write("dialog", "Dialog closed")
		} else {
			_ = m.activity.Write("dialog", fmt.Sprintf("Dialog opened (%d)", next.modal))
		}
	}
	if next.quitting && !m.quitting {
		_ = m.activity.Write("app", "Quit requested")
	}
}
func (m Model) auditSetting(key, scope, op string) {
	if m.activity != nil {
		_ = m.activity.Write("settings", strings.Join([]string{op, key, "scope=" + scope}, " "))
	}
}
