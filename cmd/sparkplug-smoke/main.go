package main

import (
	"flag"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/joyautomation/tentacle/internal/sparkplug"
)

func main() {
	broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker URL")
	group := flag.String("group", "SmokeGroup", "Sparkplug group ID")
	node := flag.String("node", "SmokeNode", "Edge node ID")
	device := flag.String("device", "SmokeDevice", "Device ID")
	count := flag.Int("count", 3, "Number of DDATA messages to publish")
	flag.Parse()

	opts := mqtt.NewClientOptions().
		AddBroker(*broker).
		SetClientID("sparkplug-smoke-publisher").
		SetCleanSession(true)
	c := mqtt.NewClient(opts)
	if t := c.Connect(); t.Wait() && t.Error() != nil {
		log.Fatalf("connect: %v", t.Error())
	}
	defer c.Disconnect(250)
	log.Printf("connected to %s", *broker)

	publish := func(topic string, payload *sparkplug.Payload) {
		bytes, err := sparkplug.EncodePayload(payload)
		if err != nil {
			log.Fatalf("encode: %v", err)
		}
		t := c.Publish(topic, 0, false, bytes)
		t.Wait()
		if t.Error() != nil {
			log.Fatalf("publish %s: %v", topic, t.Error())
		}
		log.Printf("published %s (%d bytes)", topic, len(bytes))
	}

	now := uint64(time.Now().UnixMilli())

	publish("spBv1.0/"+*group+"/NBIRTH/"+*node, &sparkplug.Payload{
		Timestamp: now, Seq: 0,
		Metrics: []sparkplug.Metric{
			sparkplug.NewBoolMetric("Node Control/Rebirth", false),
		},
	})

	publish("spBv1.0/"+*group+"/DBIRTH/"+*node+"/"+*device, &sparkplug.Payload{
		Timestamp: now, Seq: 1,
		Metrics: []sparkplug.Metric{
			sparkplug.NewDoubleMetric("Temperature", 72.5),
			sparkplug.NewDoubleMetric("Pressure", 14.7),
			sparkplug.NewBoolMetric("Running", true),
		},
	})

	for i := 0; i < *count; i++ {
		time.Sleep(500 * time.Millisecond)
		ts := uint64(time.Now().UnixMilli())
		publish("spBv1.0/"+*group+"/DDATA/"+*node+"/"+*device, &sparkplug.Payload{
			Timestamp: ts, Seq: uint64(2 + i),
			Metrics: []sparkplug.Metric{
				sparkplug.NewDoubleMetric("Temperature", 72.5+float64(i)),
				sparkplug.NewDoubleMetric("Pressure", 14.7+float64(i)*0.1),
			},
		})
	}

	time.Sleep(500 * time.Millisecond)
	log.Println("done")
}
