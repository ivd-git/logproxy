package handlers

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/influxdata/go-syslog/v2/rfc5424"
	"github.com/philips-software/go-hsdp-api/logging"
	"github.com/streadway/amqp"
	"github.com/stretchr/testify/assert"
)

type NilLogger struct {
}

func (n *NilLogger) Debugf(format string, args ...interface{}) {
	// Don't log anything
}

type NilStorer struct {
}

func (n *NilStorer) StoreResources(msgs []logging.Resource, count int) (*logging.Response, error) {
	return &logging.Response{}, nil
}

func TestProcessMessage(t *testing.T) {
	var payload = `Starting Application on 50676a99-dce0-418a-6b25-1e3d with PID 8 (/home/vcap/app/BOOT-INF/classes started by vcap in /home/vcap/app)`
	var appVersion = `1.0-f53a57a`
	var transactionID = `eea9f72c-09b6-4d56-905b-b518fc4dc5b7`

	var rawMessage = `<14>1 2018-09-07T15:39:21.132433+00:00 suite-phs.staging.msa-eustaging 7215cbaa-464d-4856-967c-fd839b0ff7b2 [APP/PROC/WEB/0] - - {"app":"msa-eustaging","val":{"message":"` + payload + `"},"ver":"` + appVersion + `","evt":null,"sev":"INFO","cmp":"CPH","trns":"` + transactionID + `","usr":null,"srv":"msa-eustaging.eu-west.philips-healthsuite.com","service":"msa","inst":"50676a99-dce0-418a-6b25-1e3d","cat":"Tracelog","time":"2018-09-07T15:39:21Z"}`

	var appName = `7215cbaa-464d-4856-967c-fd839b0ff7b2`
	var hostName = `suite-phs.staging.msa-eustaging`
	var nonDHPMessage = `<14>1 2018-09-07T15:39:18.517077+00:00 ` + hostName + ` ` + appName + ` [CELL/0] - - Starting health monitoring of container`

	parser := rfc5424.NewParser()

	phLogger, err := NewPHLogger(&NilStorer{}, &NilLogger{})
	assert.Nilf(t, err, "Expected NewPHLogger() to succeed")

	msg, err := parser.Parse([]byte(rawMessage))
	assert.Nilf(t, err, "Expected Parse() to succeed")

	resource, err := phLogger.processMessage(msg)
	assert.Nilf(t, err, "Expected processMessage() to succeed")
	assert.NotNilf(t, resource, "Proccessed resource should not be nil")
	assert.Equal(t, resource.ApplicationVersion, appVersion)
	assert.Equal(t, resource.TransactionID, transactionID)
	assert.Equal(t, resource.LogData.Message, payload)

	msg, err = parser.Parse([]byte(nonDHPMessage))

	assert.Nilf(t, err, "Expected Parse() to succeed")

	resource, err = phLogger.processMessage(msg)

	assert.Nilf(t, err, "Expected Parse() to succeed")

	assert.Equal(t, resource.LogTime, "2018-09-07T15:39:18.517Z")
	assert.Equal(t, resource.ApplicationName, appName)
	assert.Equal(t, resource.ServerName, hostName)
	assert.Equal(t, resource.LogData.Message, "Starting health monitoring of container")
}

type fakeAcknowledger struct {
	t *testing.T
}

func (a *fakeAcknowledger) Ack(tag uint64, multiple bool) error {
	return nil
}

func (a *fakeAcknowledger) Nack(tag uint64, multiple bool, requeue bool) error {
	return nil
}

func (a *fakeAcknowledger) Reject(tag uint64, requeue bool) error {
	return nil
}

func TestRFC5424Worker(t *testing.T) {
	done := make(chan bool)
	deliveries := make(chan amqp.Delivery)

	var payload = `Starting Application on 50676a99-dce0-418a-6b25-1e3d with PID 8 (/home/vcap/app/BOOT-INF/classes started by vcap in /home/vcap/app)`
	var appVersion = `1.0-f53a57a`
	var transactionID = `eea9f72c-09b6-4d56-905b-b518fc4dc5b7`
	var rawMessage = `<14>1 2018-09-07T15:39:21.132433+00:00 suite-phs.staging.msa-eustaging 7215cbaa-464d-4856-967c-fd839b0ff7b2 [APP/PROC/WEB/0] - - {"app":"msa-eustaging","val":{"message":"` + payload + `"},"ver":"` + appVersion + `","evt":null,"sev":"INFO","cmp":"CPH","trns":"` + transactionID + `","usr":null,"srv":"msa-eustaging.eu-west.philips-healthsuite.com","service":"msa","inst":"50676a99-dce0-418a-6b25-1e3d","cat":"Tracelog","time":"2018-09-07T15:39:21Z"}`

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	phLogger, err := NewPHLogger(&NilStorer{}, &NilLogger{})
	assert.Nilf(t, err, "Expected NewPHLogger() to succeed")

	go phLogger.RFC5424Worker(deliveries, done)

	fa := &fakeAcknowledger{
		t: t,
	}

	delivery := amqp.Delivery{
		Body:         []byte(rawMessage),
		Acknowledger: fa,
	}
	for i := 0; i < 25; i++ {
		deliveries <- delivery
	}
	done <- true

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	assert.Regexp(t, regexp.MustCompile("Batch flushing 25 messages"), buf.String())
}

func TestWrapResource(t *testing.T) {
	var rtrLog = `<14>1 2019-04-12T19:34:43.530045+00:00 suite-xxx.staging.mps 042cbd0f-1a0e-4f77-ae39-a5c6c9fe2af9 [RTR/6] - - mps.domain.com - [2019-04-12T19:34:43.528+0000] "GET /test/bogus HTTP/1.1" 200 0 60 "-" "Not A Health Check" "10.10.66.246:48666" "10.10.17.45:61014" x_forwarded_for:"16.19.148.81, 10.10.66.246" x_forwarded_proto:"https" vcap_request_id:"77350158-4a69-47d6-731b-1bc0678db78d" response_time:0.001628089 app_id:"042cbd0f-1a0e-4f77-ae39-a5c6c9fe2af9" app_index:"0" x_b3_traceid:"6aa3915b88798203" x_b3_spanid:"6aa3915b88798203" x_b3_parentspanid:"-"`

	parser := rfc5424.NewParser()

	phLogger, err := NewPHLogger(&NilStorer{}, &NilLogger{})

	assert.Nilf(t, err, "Expected NewPHLogger() to succeed")

	msg, err := parser.Parse([]byte(rtrLog))
	assert.Nilf(t, err, "Expected Parse() to succeed")

	resource, err := phLogger.processMessage(msg)

	assert.Nilf(t, err, "Expected processMessage() to succeed")
	assert.Equal(t, resource.LogTime, "2019-04-12T19:34:43.528Z")
}

func TestDroppedMessages(t *testing.T) {
	done := make(chan bool)
	deliveries := make(chan amqp.Delivery)

	var consulLog = `<14>1 2019-04-12T19:34:43.530045+00:00 suite-xxx.staging.mps 042cbd0f-1a0e-4f77-ae39-a5c6c9fe2af9 [RTR/6] - - mps.domain.com - [2019-04-12T19:34:43.528+0000] "GET /test/bogus HTTP/1.1" 200 0 60 "-" "Consul Health Check" "10.10.66.246:48666" "10.10.17.45:61014" x_forwarded_for:"16.19.148.81, 10.10.66.246" x_forwarded_proto:"https" vcap_request_id:"77350158-4a69-47d6-731b-1bc0678db78d" response_time:0.001628089 app_id:"042cbd0f-1a0e-4f77-ae39-a5c6c9fe2af9" app_index:"0" x_b3_traceid:"6aa3915b88798203" x_b3_spanid:"6aa3915b88798203" x_b3_parentspanid:"-"`

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	phLogger, err := NewPHLogger(&NilStorer{}, &NilLogger{})
	assert.Nilf(t, err, "Expected NewPHLogger() to succeed")

	go phLogger.RFC5424Worker(deliveries, done)

	fa := &fakeAcknowledger{
		t: t,
	}

	delivery := amqp.Delivery{
		Body:         []byte(consulLog),
		Acknowledger: fa,
	}
	for i := 0; i < 25; i++ {
		deliveries <- delivery
	}
	time.Sleep(1100 * time.Millisecond) // Wait for the flush to happen

	done <- true

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	assert.Regexp(t, regexp.MustCompile("Dropped 25 messages"), buf.String())
}
