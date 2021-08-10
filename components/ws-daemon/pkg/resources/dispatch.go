// Copyright (c) 2020 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License-AGPL.txt in the project root for license information.

package resources

import (
	"context"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/xerrors"
	"k8s.io/apimachinery/pkg/api/resource"

	wsk8s "github.com/gitpod-io/gitpod/common-go/kubernetes"
	"github.com/gitpod-io/gitpod/common-go/log"
	"github.com/gitpod-io/gitpod/ws-daemon/pkg/container"
	"github.com/gitpod-io/gitpod/ws-daemon/pkg/dispatch"
)

// Config configures the containerd resource governer dispatch
type Config struct {
	CPULimiter        CPULimiterConfig    `json:"cpuLimiter"`
	CPUBuckets        []Bucket            `json:"cpuBuckets"`
	ControlPeriod     string              `json:"controlPeriod"`
	SamplingPeriod    string              `json:"samplingPeriod"`
	CGroupsBasePath   string              `json:"cgroupBasePath"`
	ProcessPriorities map[ProcessType]int `json:"processPriorities"`
}

type CPULimiterConfig struct {
	Kind              CPULimiterKind                 `json:"kind"`
	Bucket            []Bucket                       `json:"bucket"`
	BudgetedGlobalUse BudgetedGlobalUseLimiterConfig `json:"budgetedGlobalUse"`
}

type CPULimiterKind string

const (
	CPULimiterBucket            CPULimiterKind = "bucket"
	CPULimiterBudgetedGlobalUse CPULimiterKind = "budgetedGlobalUse"
)

// NewCPULimiter produces a new limiter from configuration
func NewCPULimiter(cfg *CPULimiterConfig) (ResourceLimiter, error) {
	switch cfg.Kind {
	case CPULimiterBucket:
		return BucketLimiter(cfg.Bucket), nil
	case CPULimiterBudgetedGlobalUse:
		stat, err := defaultSystemStat()
		if err != nil {
			return nil, err
		}
		return &BudgetedGlobalUseLimiter{
			Config: cfg.BudgetedGlobalUse,
			Stat:   stat,
		}, nil
	case "":
		// no limiter configured - that's fine
		return nil, nil
	}
	return nil, fmt.Errorf("unknown limiter %s", cfg.Kind)
}

// NewDispatchListener creates a new resource governer dispatch listener
func NewDispatchListener(cfg *Config, prom prometheus.Registerer) *DispatchListener {
	d := &DispatchListener{
		Prometheus: prom,
		Config:     cfg,
		governer:   make(map[container.ID]*Controller),
	}
	prom.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "resource_governer_total",
			Help: "Number active workspace resource governer",
		}, func() float64 {
			d.mu.Lock()
			defer d.mu.Unlock()

			return float64(len(d.governer))
		}),
	)

	return d
}

// DispatchListener starts new resource governer using the workspace dispatch
type DispatchListener struct {
	Prometheus prometheus.Registerer
	Config     *Config

	governer map[container.ID]*Controller
	mu       sync.Mutex
}

// WorkspaceAdded starts new governer
func (d *DispatchListener) WorkspaceAdded(ctx context.Context, ws *dispatch.Workspace) error {
	d.mu.Lock()
	if _, ok := d.governer[ws.ContainerID]; ok {
		d.mu.Unlock()
		return nil
	}
	defer d.mu.Unlock()

	disp := dispatch.GetFromContext(ctx)
	if disp == nil {
		return xerrors.Errorf("no dispatch available")
	}

	cgroupPath, err := disp.Runtime.ContainerCGroupPath(context.Background(), ws.ContainerID)
	if err != nil {
		return xerrors.Errorf("cannot start governer: %w", err)
	}

	var cpuLimiter ResourceLimiter
	if fixedLimit, ok := ws.Pod.Annotations[wsk8s.CPULimitAnnotation]; ok && fixedLimit != "" {
		var scaledLimit int64
		limit, err := resource.ParseQuantity(fixedLimit)
		if err != nil {
			log.WithError(err).WithField("limitReq", fixedLimit).Warn("workspace requested a fixed CPU limit, but we cannot parse the value")
		}
		// we need to scale from milli jiffie to jiffie - see governer code for details
		scaledLimit = limit.MilliValue() / 10
		cpuLimiter = FixedLimiter(scaledLimit)
	} else if len(d.Config.CPUBuckets) > 0 {
		// Deprecated behaviour to avoid config surface breakage.
		// TODO(cw): remove once all config has been migrated.
		log.Warn("using deprecated cpuBuckets config - please switch to cpuLimiter")
		cpuLimiter = &ClampingBucketLimiter{Buckets: d.Config.CPUBuckets}
	} else {
		cpuLimiter, err = NewCPULimiter(&d.Config.CPULimiter)
		if err != nil {
			return err
		}
	}

	log := log.WithFields(wsk8s.GetOWIFromObject(&ws.Pod.ObjectMeta)).WithField("containerID", ws.ContainerID)
	g, err := NewController(string(ws.ContainerID), ws.InstanceID, cgroupPath,
		WithCGroupBasePath(d.Config.CGroupsBasePath),
		WithCPULimiter(cpuLimiter),
		WithGitpodIDs(ws.WorkspaceID, ws.InstanceID),
		WithPrometheusRegisterer(prometheus.WrapRegistererWith(prometheus.Labels{"instanceId": ws.InstanceID}, d.Prometheus)),
		WithProcessPriorities(d.Config.ProcessPriorities),
	)
	if err != nil {
		return xerrors.Errorf("cannot start governer: %w", err)
	}

	d.governer[ws.ContainerID] = g
	go g.Start(ctx)
	log.Info("started new resource governer")

	return nil
}

// WorkspaceUpdated gets called when a workspace is updated
func (d *DispatchListener) WorkspaceUpdated(ctx context.Context, ws *dispatch.Workspace) error {
	d.mu.Lock()
	gov, ok := d.governer[ws.ContainerID]
	d.mu.Unlock()
	if !ok {
		return nil
	}

	newCPULimit := ws.Pod.Annotations[wsk8s.CPULimitAnnotation]
	var scaledLimit int64
	if newCPULimit != "" {
		limit, err := resource.ParseQuantity(newCPULimit)
		if err != nil {
			return xerrors.Errorf("cannot enforce fixed CPU limit: %w", err)
		}
		// we need to scale from milli jiffie to jiffie - see governer code for details
		scaledLimit = limit.MilliValue() / 10
	}

	gov.SetFixedCPULimit(scaledLimit)
	return nil
}
