package metrics

import (
	"encoding/json"
	"github.com/openziti/foundation/metrics/metrics_pb"
	"github.com/openziti/foundation/util/iomonad"
	"io"
	"strings"
)

type Formatter interface {
	WriteTo(msg *metrics_pb.MetricsMessage, out io.Writer) error
}

type PlainTextFormatter struct {
}

func (PlainTextFormatter) WriteTo(msg *metrics_pb.MetricsMessage, out io.Writer) error {
	w := iomonad.Wrap(out)
	for name, val := range msg.IntValues {
		w.Printf("%v: %9d\n", name, val)
	}

	for name, val := range msg.FloatValues {
		w.Printf("%s: %v\n", name, val)
	}

	for name, val := range msg.Histograms {
		w.Printf("histogram %s\n", name)
		w.Printf("  count:       %9d\n", val.Count)
		w.Printf("  min:         %9d\n", val.Min)
		w.Printf("  max:         %9d\n", val.Max)
		w.Printf("  mean:        %12.2f\n", val.Mean)
		w.Printf("  stddev:      %12.2f\n", val.StdDev)
		w.Printf("  50%%:         %12.2f\n", val.P50)
		w.Printf("  75%%:         %12.2f\n", val.P75)
		w.Printf("  95%%:         %12.2f\n", val.P95)
		w.Printf("  99%%:         %12.2f\n", val.P99)
		w.Printf("  99.9%%:       %12.2f\n", val.P999)
		w.Printf("  99.99%%:      %12.2f\n", val.P9999)
	}

	// meters
	for name, val := range msg.Meters {
		w.Printf("meter %s\n", name)
		w.Printf("  count:       %9d\n", val.Count)
		w.Printf("  1-min rate:  %12.2f\n", val.M1Rate)
		w.Printf("  5-min rate:  %12.2f\n", val.M5Rate)
		w.Printf("  15-min rate: %12.2f\n", val.M15Rate)
		w.Printf("  mean rate:   %12.2f\n", val.MeanRate)
	}

	// timers
	for name, val := range msg.Timers {
		w.Printf("timer %s\n", name)
		w.Printf("  count:       %9d\n", val.Count)
		w.Printf("  min:         %9d\n", val.Min)
		w.Printf("  max:         %9d\n", val.Max)
		w.Printf("  mean:        %12.2f\n", val.Mean)
		w.Printf("  stddev:      %12.2f\n", val.StdDev)
		w.Printf("  50%%:         %12.2f\n", val.P50)
		w.Printf("  75%%:         %12.2f\n", val.P75)
		w.Printf("  95%%:         %12.2f\n", val.P95)
		w.Printf("  99%%:         %12.2f\n", val.P99)
		w.Printf("  99.9%%:       %12.2f\n", val.P999)
		w.Printf("  99.99%%:      %12.2f\n", val.P9999)
		w.Printf("  1-min rate:  %12.2f\n", val.M1Rate)
		w.Printf("  5-min rate:  %12.2f\n", val.M5Rate)
		w.Printf("  15-min rate: %12.2f\n", val.M15Rate)
		w.Printf("  mean rate:   %12.2f\n", val.MeanRate)
	}

	return w.GetError()
}

type JsonFormatter struct{}

func (formatter *JsonFormatter) WriteTo(msg *metrics_pb.MetricsMessage, out io.Writer) error {
	links := make(map[string]interface{})
	event := make(map[string]interface{})
	tags := make(map[string]string)

	// inject tags if there are any
	for k, v := range msg.Tags {
		tags[k] = v
	}

	event["tags"] = tags
	event["sourceId"] = msg.SourceId
	event["timestamp"] = msg.Timestamp.Seconds * 1000

	// ints
	for name, val := range msg.IntValues {
		// if link is in the name, add it to the link event
		if strings.Contains(name, "link") {
			links = formatter.processLinkEvent(links, name, val)
			continue
		}
		event[name] = val
	}

	// floats
	for name, val := range msg.FloatValues {
		// if link is in the name, add it to the link event
		if strings.Contains(name, "link") {
			links = formatter.processLinkEvent(links, name, val)
			continue
		}

		event[name] = val
	}

	// meters
	for name, val := range msg.Meters {
		// if link is in the name, add it to the link event
		if strings.Contains(name, "link") {
			links = formatter.processLinkEvent(links, name, val)
			continue
		}

		attrMap := map[string]interface{}{}
		attrMap["count"] = val.Count
		attrMap["m1_rate"] = val.M1Rate
		attrMap["m5_rate"] = val.M5Rate
		attrMap["m15_rate"] = val.M15Rate
		attrMap["mean_rate"] = val.MeanRate
		event[name] = attrMap
	}

	// histograms
	for name, val := range msg.Histograms {
		// if link is in the name, add it to the link event
		if strings.Contains(name, "link") {
			links = formatter.processLinkEvent(links, name, val)
			continue
		}

		attrMap := map[string]interface{}{}
		attrMap["count"] = val.Count
		attrMap["min"] = val.Min
		attrMap["max"] = val.Max
		attrMap["mean"] = val.Mean
		attrMap["stddev"] = val.StdDev
		attrMap["p50"] = val.P50
		attrMap["p75"] = val.P75
		attrMap["p95"] = val.P95
		attrMap["p99"] = val.P99
		attrMap["p999"] = val.P999
		attrMap["p9999"] = val.P9999
		event[name] = attrMap
	}

	// timers
	for name, val := range msg.Timers {
		// if link is in the name, add it to the link event
		if strings.Contains(name, "link") {
			links = formatter.processLinkEvent(links, name, val)
			continue
		}

		attrMap := map[string]interface{}{}
		attrMap["count"] = val.Count
		attrMap["m1_rate"] = val.M1Rate
		attrMap["m5_rate"] = val.M5Rate
		attrMap["m15_rate"] = val.M15Rate
		attrMap["mean_rate"] = val.MeanRate
		attrMap["min"] = val.Min
		attrMap["max"] = val.Max
		attrMap["mean"] = val.Mean
		attrMap["stddev"] = val.StdDev
		attrMap["p50"] = val.P50
		attrMap["p75"] = val.P75
		attrMap["p95"] = val.P95
		attrMap["p99"] = val.P99
		attrMap["p999"] = val.P999
		attrMap["p9999"] = val.P9999
		event[name] = attrMap
	}

	// intervals
	for name, val := range msg.IntervalCounters {
		// if link is in the name, add it to the link event
		if strings.Contains(name, "link") {
			links = formatter.processLinkEvent(links, name, val)
			continue
		}

		event[name] = val
	}

	w := iomonad.Wrap(out)
	// json format
	marshalled, err := json.Marshal(event)
	if err != nil {
		return err
	}
	w.Write(marshalled)
	w.Println("")

	for linkName, linkEvent := range links {
		// build an event just for the link that can be rolled up by router id
		linkEvent.(map[string]interface{})["linkId"] = linkName
		linkEvent.(map[string]interface{})["sourceId"] = msg.SourceId
		linkEvent.(map[string]interface{})["timestamp"] = msg.Timestamp.Seconds * 1000
		linkEvent.(map[string]interface{})["tags"] = tags

		// json format
		marshalled, err = json.Marshal(linkEvent)
		if err != nil {
			return err
		}

		w.Write(marshalled)
		w.Println("")
	}

	return w.GetError()
}

func (JsonFormatter) processLinkEvent(links map[string]interface{}, name string, val interface{}) map[string]interface{} {
	parts := strings.Split(name, ".")
	linkName := string(parts[1])

	//rename the metric key without the id in it
	metricName := strings.Replace(name, linkName+".", "", 1)

	// see if the link event exists, if not create it
	_, exists := links[linkName]

	if !exists {
		links[linkName] = make(map[string]interface{})
	}

	links[linkName].(map[string]interface{})[metricName] = val

	return links
}
