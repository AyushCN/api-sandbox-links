package orchestrator

import (
	"bytes"
	"context"
	"fmt"
)

// PlanAnalyze delegates repository inspection and plan detection to the configured builder.
func (o *Orchestrator) PlanAnalyze(ctx context.Context, dir string) (*BuildPlan, error) {
	if o.builder == nil {
		return nil, fmt.Errorf("orchestrator: no builder configured")
	}
	return o.builder.PlanAnalyze(ctx, dir)
}

// BuildImage delegates container image compilation to the configured builder.
func (o *Orchestrator) BuildImage(ctx context.Context, dir, name string, progress *bytes.Buffer) (string, error) {
	if o.builder == nil {
		return "", fmt.Errorf("orchestrator: no builder configured")
	}
	return o.builder.BuildImage(ctx, dir, name, progress)
}

// Builder returns the configured Builder implementation.
func (o *Orchestrator) Builder() Builder {
	return o.builder
}

// SetBuilder overrides the orchestrator's builder (useful in tests or specialized configurations).
func (o *Orchestrator) SetBuilder(b Builder) {
	o.builder = b
}
