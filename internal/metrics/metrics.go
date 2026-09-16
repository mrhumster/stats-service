package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	// ReactionsTotal counts reaction upserts split by kind (like/dislike/none).
	ReactionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "reactions_total",
		Help: "Number of reaction mutations by kind",
	}, []string{"kind"})

	// ViewsTotal counts registered stream views.
	ViewsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "views_total",
		Help: "Number of registered stream views",
	}, []string{"status"})
)

func init() {
	prometheus.MustRegister(ReactionsTotal, ViewsTotal)
}

func Reaction(kind string) { ReactionsTotal.WithLabelValues(kind).Inc() }
func View(status string)   { ViewsTotal.WithLabelValues(status).Inc() }