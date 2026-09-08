package outbox

import kafka "github.com/segmentio/kafka-go"

// KafkaHeaderCarrier adapts a *[]kafka.Header to OpenTelemetry's
// propagation.TextMapCarrier, so a span's context can be injected into
// (Publisher) or extracted from (internal/audit.Consumer, which already
// imports this package for its shared event-type constants) a Kafka
// message's headers — spec section 36's "Kafka" hop.
type KafkaHeaderCarrier struct {
	Headers *[]kafka.Header
}

func (c KafkaHeaderCarrier) Get(key string) string {
	for _, h := range *c.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c KafkaHeaderCarrier) Set(key, value string) {
	for i, h := range *c.Headers {
		if h.Key == key {
			(*c.Headers)[i].Value = []byte(value)
			return
		}
	}
	*c.Headers = append(*c.Headers, kafka.Header{Key: key, Value: []byte(value)})
}

func (c KafkaHeaderCarrier) Keys() []string {
	keys := make([]string, len(*c.Headers))
	for i, h := range *c.Headers {
		keys[i] = h.Key
	}
	return keys
}
