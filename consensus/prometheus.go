// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

import "github.com/prometheus/client_golang/prometheus"

var (
	pmRoundGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pacemaker_round",
		Help: "Current round of pacemaker",
	})
	curEpochGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "current_epoch",
		Help: "Current epoch of consensus",
	})
	inCommitteeGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "in_committee",
		Help: "is this node in committee",
	})
	pmRoleGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pacemaker_role",
		Help: "Role in pacemaker",
	})
)
