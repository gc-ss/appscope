package cmd

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	linq "github.com/ahmetb/go-linq/v3"
	"github.com/criblio/scope/metrics"
	"github.com/criblio/scope/util"
	"github.com/guptarohit/asciigraph"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh/terminal"
)

// metricsCmd represents the metrics command
var metricsCmd = &cobra.Command{
	Use:     "metrics [flags]",
	Short:   "Outputs metrics for a session",
	Long:    `Outputs metrics for a session`,
	Example: `scope metrics`,
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		id, _ := cmd.Flags().GetInt("id")
		names, _ := cmd.Flags().GetStringSlice("metric")
		graph, _ := cmd.Flags().GetBool("graph")
		cols, _ := cmd.Flags().GetBool("cols")
		uniq, _ := cmd.Flags().GetBool("uniq")

		sessions := sessionByID(id)

		if graph && len(names) == 0 {
			helpErrAndExit(cmd, "Must specify a metric names with --graph")
		} else if cols && len(names) == 0 {
			helpErrAndExit(cmd, "Must specify metric names with --cols")
		}

		file, err := os.Open(sessions[0].MetricsPath)
		if err != nil && strings.Contains(err.Error(), "metrics.json: no such file or directory") {
			promptClean(sessions[0:1])
		}
		in := make(chan metrics.Metric)
		offsetChan := make(chan int)
		filters := []util.MatchFunc{}
		if len(names) > 0 {
			for _, n := range names {
				filters = append(filters, util.MatchString(n))
			}
		} else {
			filters = []util.MatchFunc{util.MatchAlways}
		}
		go func() {
			offset, err := metrics.Reader(file, util.MatchAny(filters...), in)
			util.CheckErrSprintf(err, "error reading metrics: %v", err)
			offsetChan <- offset
		}()
		util.CheckErrSprintf(err, "error reading metrics from file: %v", err)

		// Filter metrics to match metrics
		values := []float64{}
		metricCols := []map[string]interface{}{}
		mm := []metrics.Metric{}

		firstMetric := ""
		for m := range in {
			if firstMetric == "" {
				firstMetric = m.Name
			}
			if cols && m.Name == firstMetric { // use first metric as a breaker for batches of metrics
				metricCols = append(metricCols, map[string]interface{}{})
			}
			if len(names) > 0 {
				for _, name := range names {
					if m.Name == name {
						if graph {
							values = append(values, m.Value)
						} else if cols {
							metricCols[len(metricCols)-1][m.Name] = m.Value
						} else {
							mm = append(mm, m)
						}
					}
				}
			} else {
				mm = append(mm, m)
			}
		}

		if graph {
			termWidth, _, err := terminal.GetSize(0)
			util.CheckErrSprintf(err, "error getting terminal width: %v", err)

			q := linq.From(values)
			max := q.Max().(float64)
			legendSize := len(fmt.Sprintf("%.0f", max)) + 4
			maxValues := termWidth - legendSize
			sampleRate := int(math.Round(float64(len(values)) / float64(maxValues)))
			if sampleRate == 0 {
				sampleRate = 1
			}

			var newValues []float64
			q.WhereIndexed(
				func(idx int, _ interface{}) bool {
					return idx%sampleRate == 0
				},
			).ToSlice(&newValues)
			if maxValues > len(newValues) {
				maxValues = len(newValues)
			}
			fmt.Println(asciigraph.Plot(newValues[:maxValues], asciigraph.Height(20)))
			os.Exit(0)
		}

		if cols {
			ofCols := []util.ObjField{}
			for _, col := range names {
				ofCols = append(ofCols, util.ObjField{Name: col, Field: col})
			}
			util.PrintObj(ofCols, metricCols)
			os.Exit(0)
		}

		if uniq {
			linq.From(mm).DistinctBy(func(item interface{}) interface{} {
				return item.(metrics.Metric).Name
			}).ToSlice(&mm)
		}

		util.PrintObj([]util.ObjField{
			{Name: "Name", Field: "name"},
			{Name: "Value", Field: "value"},
			{Name: "Type", Field: "type"},
			{Name: "Unit", Field: "unit"},
			{Name: "PID", Field: "pid"},
			{Name: "Tags", Field: "tags", Transform: func(obj interface{}) string {
				ret := ""
				tags := obj.([]metrics.MetricTag)
				sort.Slice(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })
				for _, t := range tags {
					ret += fmt.Sprintf("%s: %s,", t.Name, t.Value)
				}
				if len(ret) > 0 {
					ret = ret[:len(ret)-1]
				}
				return ret
			}},
		}, mm)
	},
}

func init() {
	metricsCmd.Flags().IntP("id", "i", -1, "Display info from specific from session ID")
	metricsCmd.Flags().StringSliceP("metric", "m", []string{}, "Display only supplied metrics")
	metricsCmd.Flags().BoolP("graph", "g", false, "Graph this metric")
	metricsCmd.Flags().BoolP("cols", "c", false, "Display metrics as columns")
	metricsCmd.Flags().BoolP("uniq", "u", false, "Display first instance of each unique metric")
	RootCmd.AddCommand(metricsCmd)
}