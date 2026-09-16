package recorder

import (
	"context"
	"testing"

	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
)

type recordedCall struct {
	method     string
	ctx        context.Context
	reason     string
	message    string
	messageFmt string
	args       []interface{}
}

type fakeRecorder struct {
	component string
	calls     []recordedCall
}

func (f *fakeRecorder) Event(ctx context.Context, reason, message string) {
	f.calls = append(f.calls, recordedCall{method: "Event", ctx: ctx, reason: reason, message: message})
}

func (f *fakeRecorder) Eventf(ctx context.Context, reason, messageFmt string, args ...interface{}) {
	f.calls = append(f.calls, recordedCall{method: "Eventf", ctx: ctx, reason: reason, messageFmt: messageFmt, args: args})
}

func (f *fakeRecorder) Warning(ctx context.Context, reason, message string) {
	f.calls = append(f.calls, recordedCall{method: "Warning", ctx: ctx, reason: reason, message: message})
}

func (f *fakeRecorder) Warningf(ctx context.Context, reason, messageFmt string, args ...interface{}) {
	f.calls = append(f.calls, recordedCall{method: "Warningf", ctx: ctx, reason: reason, messageFmt: messageFmt, args: args})
}

func (f *fakeRecorder) ForComponent(componentName string) events.Recorder {
	return &fakeRecorder{component: componentName}
}

func (f *fakeRecorder) WithComponentSuffix(suffix string) events.Recorder {
	return f.ForComponent(f.component + "-" + suffix)
}

func (f *fakeRecorder) ComponentName() string {
	return f.component
}

func (f *fakeRecorder) Shutdown() {}

func TestEventsRecorderWrapperDelegation(t *testing.T) {
	f := &fakeRecorder{component: "test"}
	ctx := context.Background()
	w := NewEventsRecorderWrapper(ctx, f)

	w.Event("Created", "created a thing")
	w.Eventf("Updated", "updated %d things", 3)
	w.Warning("Failed", "failed a thing")
	w.Warningf("Retrying", "retrying %d times", 2)

	expected := []recordedCall{
		{method: "Event", ctx: ctx, reason: "Created", message: "created a thing"},
		{method: "Eventf", ctx: ctx, reason: "Updated", messageFmt: "updated %d things", args: []interface{}{3}},
		{method: "Warning", ctx: ctx, reason: "Failed", message: "failed a thing"},
		{method: "Warningf", ctx: ctx, reason: "Retrying", messageFmt: "retrying %d times", args: []interface{}{2}},
	}
	if len(f.calls) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(f.calls), f.calls)
	}
	for i, want := range expected {
		got := f.calls[i]
		if got.method != want.method || got.ctx != want.ctx || got.reason != want.reason ||
			got.message != want.message || got.messageFmt != want.messageFmt || len(got.args) != len(want.args) {
			t.Errorf("call %d: expected %+v, got %+v", i, want, got)
			continue
		}
		for j := range want.args {
			if got.args[j] != want.args[j] {
				t.Errorf("call %d arg %d: expected %v, got %v", i, j, want.args[j], got.args[j])
			}
		}
	}
}

func TestEventsRecorderWrapperComponentName(t *testing.T) {
	f := &fakeRecorder{component: "hub-controller"}
	w := NewEventsRecorderWrapper(context.Background(), f)

	if got := w.ComponentName(); got != "hub-controller" {
		t.Errorf("expected component name %q, got %q", "hub-controller", got)
	}
}

func TestEventsRecorderWrapperForComponentDoesNotMutateOriginal(t *testing.T) {
	f := &fakeRecorder{component: "hub-controller"}
	w := NewEventsRecorderWrapper(context.Background(), f)

	sub := w.ForComponent("sub-reconciler")

	if got := w.ComponentName(); got != "hub-controller" {
		t.Errorf("original wrapper's component name changed, expected %q, got %q", "hub-controller", got)
	}
	if got := sub.ComponentName(); got != "sub-reconciler" {
		t.Errorf("expected new wrapper's component name %q, got %q", "sub-reconciler", got)
	}

	sub.Event("Created", "from sub")
	if len(f.calls) != 0 {
		t.Errorf("original recorder should not receive events routed to a sub-component, got %v", f.calls)
	}

	w.Event("Created", "from original")
	if len(f.calls) != 1 {
		t.Errorf("original recorder should still receive its own events, got %v", f.calls)
	}
}

func TestEventsRecorderWrapperWithComponentSuffix(t *testing.T) {
	f := &fakeRecorder{component: "hub-controller"}
	w := NewEventsRecorderWrapper(context.Background(), f)

	sub := w.WithComponentSuffix("retry")

	if got := sub.ComponentName(); got != "hub-controller-retry" {
		t.Errorf("expected suffixed component name %q, got %q", "hub-controller-retry", got)
	}
	if got := w.ComponentName(); got != "hub-controller" {
		t.Errorf("original wrapper's component name changed, expected %q, got %q", "hub-controller", got)
	}
}

func TestEventsRecorderWrapperWithContextDoesNotMutateOriginal(t *testing.T) {
	f := &fakeRecorder{component: "test"}
	original := NewEventsRecorderWrapper(context.Background(), f)

	type ctxKey struct{}
	newCtx := context.WithValue(context.Background(), ctxKey{}, "child")
	withNewCtx := original.WithContext(newCtx)

	withNewCtx.Event("Created", "via new context")
	original.Event("Created", "via original context")

	if len(f.calls) != 2 {
		t.Fatalf("expected 2 recorded calls, got %d: %v", len(f.calls), f.calls)
	}
	if f.calls[0].ctx.Value(ctxKey{}) != "child" {
		t.Errorf("expected the recorder to receive the new context, got %v", f.calls[0].ctx)
	}
	if f.calls[1].ctx.Value(ctxKey{}) != nil {
		t.Errorf("expected the recorder to receive the original context, got %v", f.calls[1].ctx)
	}

	originalWrapper, ok := original.(*EventsRecorderWrapper)
	if !ok {
		t.Fatalf("expected *EventsRecorderWrapper, got %T", original)
	}
	if originalWrapper.ctx.Value(ctxKey{}) != nil {
		t.Errorf("WithContext mutated the original wrapper's context")
	}

	withNewCtxWrapper, ok := withNewCtx.(*EventsRecorderWrapper)
	if !ok {
		t.Fatalf("expected *EventsRecorderWrapper, got %T", withNewCtx)
	}
	if withNewCtxWrapper.ctx.Value(ctxKey{}) != "child" {
		t.Errorf("expected new wrapper to use the new context")
	}
}
