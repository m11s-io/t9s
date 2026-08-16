package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	expectedServiceRowLimit         = 100
	expectedServiceDetailEventWidth = 80
)

func TestServicesRenderAllNodeRowsAndHealthSymbols(t *testing.T) {
	rendered := renderServices(120, servicesTestState())

	assert.Contains(t, rendered, "NODE")
	assert.Contains(t, rendered, "SERVICE")
	assert.Contains(t, rendered, "STATE")
	assert.Contains(t, rendered, "HEALTH")
	assert.Contains(t, rendered, "LAST EVENT")
	for _, value := range []string{"node-alpha", "node-beta", "etcd", "kubelet", "machined", "✓", "!", "?"} {
		assert.Contains(t, rendered, value)
	}
}

func TestServicesRenderUnknownHealthAsQuestionMark(t *testing.T) {
	assert.Equal(t, "?", serviceHealthSymbol(nil))
}

func TestServicesFilterMatchesNodeAndServiceCaseInsensitively(t *testing.T) {
	services := newServicesModel(servicesTestState())
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('N'), keyPress('o'), keyPress('D'), keyPress('e'), keyPress('-'), keyPress('b'), keyPress('E'), keyPress('t'), keyPress('A'),
		{Code: tea.KeyEnter},
	} {
		services = services.update(message)
	}

	filtered := services.view(120)
	assert.Contains(t, filtered, "node-beta")
	assert.Contains(t, filtered, "machined")
	assert.NotContains(t, filtered, "node-alpha")
	assert.NotContains(t, filtered, "kubelet")
	assert.Contains(t, filtered, "FILTER NoDe-bEtA")

	services = services.update(tea.KeyPressMsg{Code: tea.KeyEsc})
	cleared := services.view(120)
	assert.Contains(t, cleared, "node-alpha")
	assert.Contains(t, cleared, "node-beta")
	assert.NotContains(t, cleared, "FILTER")
}

func TestServicesRenderEmptyState(t *testing.T) {
	rendered := renderServices(80, application.ServiceState{Status: application.Ready})

	assert.Contains(t, rendered, "No services")
}

func TestServicesSelectionResetsWhenRefreshedServiceSetIsEmpty(t *testing.T) {
	services := newServicesModel(application.ServiceState{
		Status: application.Ready,
		Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{
			{Node: "cp-1", Name: "etcd"},
			{Node: "cp-1", Name: "kubelet"},
		}},
	})
	services = services.update(tea.KeyPressMsg{Code: tea.KeyDown})

	services = services.setState(application.ServiceState{Status: application.Ready})

	_, ok := services.selected()
	assert.False(t, ok)
	assert.Equal(t, "No services", services.viewSized(contentSize{Width: 80, Height: 10}))
}

func TestServicesRenderCapsTheNumberOfRows(t *testing.T) {
	state := application.ServiceState{Status: application.Ready}
	for index := 0; index < expectedServiceRowLimit+1; index++ {
		state.Value.Services = append(state.Value.Services, domain.ServiceSnapshot{Node: "cp-1", Name: fmt.Sprintf("service-%03d", index)})
	}

	rendered := renderServices(120, state)

	assert.Contains(t, rendered, "service-099")
	assert.NotContains(t, rendered, "service-100")
	assert.Equal(t, expectedServiceRowLimit+1, len(strings.Split(rendered, "\n")))
}

func TestServicesRenderSuccessfulRowsWithNodeScopedProblems(t *testing.T) {
	state := application.ServiceState{Status: application.Partial, Value: domain.ServiceSet{
		Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "etcd", State: "Running"}},
		Problems: []domain.ServiceProblem{{Node: "worker-1", Message: "services unavailable"}},
	}}
	model := newServicesModel(state)

	view := model.viewSized(contentSize{Width: 100, Height: 8})

	assert.Contains(t, view, "cp-1")
	assert.Contains(t, view, "etcd")
	assert.Contains(t, view, "worker-1")
	assert.Contains(t, view, "services unavailable")
}

func TestServicesFilterMatchesNodeScopedProblems(t *testing.T) {
	state := application.ServiceState{Status: application.Partial, Value: domain.ServiceSet{
		Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "etcd"}},
		Problems: []domain.ServiceProblem{{Node: "worker-1", Message: "services unavailable"}},
	}}
	model := newServicesModel(state).startFilter("WoRkEr")

	view := model.viewSized(contentSize{Width: 100, Height: 8})

	assert.Contains(t, view, "worker-1")
	assert.Contains(t, view, "services unavailable")
	assert.NotContains(t, view, "etcd")
	assert.NotContains(t, view, "No services")
}

func TestServiceDetailTruncatesTheEventField(t *testing.T) {
	rendered := renderServiceDetail(domain.ServiceSnapshot{LastMessage: strings.Repeat("event ", expectedServiceDetailEventWidth)})
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, "LAST EVENT ") {
			event := strings.TrimPrefix(line, "LAST EVENT ")
			assert.LessOrEqual(t, ansi.StringWidth(event), expectedServiceDetailEventWidth)
			assert.True(t, strings.HasSuffix(event, "…"))
			return
		}
	}
	t.Fatal("service detail did not render a LAST EVENT field")
}

func TestServicesEnterShowsSelectedServiceDetail(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Services: servicesTestState()}, nil)
	root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})

	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyDown})
	root, command := updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Nil(t, command)
	assert.Contains(t, root.View().Content, "SERVICE DETAIL")
	assert.Contains(t, root.View().Content, "NODE       node-alpha")
	assert.Contains(t, root.View().Content, "SERVICE    kubelet")
}

func TestServicesEscapeReturnsToListAndKeepsRootResource(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Services: servicesTestState()}, nil)
	root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	root, command := updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Nil(t, command)
	assert.Contains(t, root.View().Content, "NODE")
	assert.NotContains(t, root.View().Content, "SERVICE DETAIL")

	root, command = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Nil(t, command)
	assert.Contains(t, root.View().Content, "LAST EVENT")
	assert.NotContains(t, root.View().Content, "SERVICE DETAIL")
}

func TestServicesGolden(t *testing.T) {
	for _, width := range []int{80, 120} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			got := trimGoldenLines(renderServices(width, servicesTestState())) + "\n"
			path := filepath.Join("testdata", "services-"+strconv.Itoa(width)+".golden")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
			}

			want, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(want), got)
			for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
				assert.LessOrEqual(t, ansi.StringWidth(line), width)
			}
		})
	}
}

func servicesTestState() application.ServiceState {
	truth := true
	falsity := false
	return application.ServiceState{
		Status: application.Ready,
		Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{
			{Node: "node-alpha", Name: "etcd", State: "Running", Healthy: &truth, LastMessage: "member is healthy", LastChange: time.Date(2026, time.August, 14, 10, 30, 0, 0, time.UTC)},
			{Node: "node-alpha", Name: "kubelet", State: "Failed", Healthy: &falsity, LastMessage: "failed to start after a deliberately long event message"},
			{Node: "node-beta", Name: "machined", State: "Running", LastMessage: "health is not reported"},
		}},
	}
}
