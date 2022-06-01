package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/go-logr/logr"
	"go.seankhliao.com/gchat"
	"go.seankhliao.com/svcrunner"
	"go.seankhliao.com/svcrunner/envflag"
	cloudbuildpb "google.golang.org/genproto/googleapis/devtools/cloudbuild/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

type Server struct {
	webhookURL string
	webhook    *gchat.WebhookClient
	log        logr.Logger
}

func New(hs *http.Server) *Server {
	s := &Server{}
	hs.Handler = s
	return s
}

func (s *Server) Register(c *envflag.Config) {
	c.StringVar(&s.webhookURL, "gchat.webhook", "", "webhook endpoint for google chat")
}

func (s *Server) Init(ctx context.Context, t svcrunner.Tools) error {
	s.log = t.Log.WithName("gchat")
	s.webhook = &gchat.WebhookClient{Endpoint: s.webhookURL, Client: http.DefaultClient}
	return nil
}

func (s *Server) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		s.log.Error(err, "bad request")
		http.Error(rw, "read request", http.StatusBadRequest)
		return
	}
	var msg PubSubMessage
	err = json.Unmarshal(b, &msg)
	if err != nil {
		s.log.Error(err, "umarshal pubsub")
		http.Error(rw, "parse request", http.StatusBadRequest)
		return
	}

	var build cloudbuildpb.Build
	err = protojson.Unmarshal(msg.Message.Data, &build)
	if err != nil {
		s.log.Error(err, "umarshal build")
		http.Error(rw, "parse request", http.StatusBadRequest)
		return
	}
	switch build.Status {
	case cloudbuildpb.Build_CANCELLED,
		cloudbuildpb.Build_TIMEOUT,
		cloudbuildpb.Build_FAILURE,
		cloudbuildpb.Build_SUCCESS:
		break
	default:
		s.log.Info("ignoring status", "status", build.Status)
		rw.WriteHeader(http.StatusOK)
		return
	}
	buildDuration := build.FinishTime.AsTime().Sub(build.StartTime.AsTime())

	err = s.webhook.Post(ctx, gchat.WebhookPayload{
		Text: fmt.Sprintf(`%s | %s | %v\n<%s|build log>`,
			build.Status, build.BuildTriggerId, buildDuration, build.LogUrl,
		),
	})
	if err != nil {
		s.log.Error(err, "post chat msg")
		http.Error(rw, "post msg", http.StatusInternalServerError)
		return
	}
	rw.WriteHeader(http.StatusOK)
}

// https://cloud.google.com/pubsub/docs/reference/rest/v1/PubsubMessage
type PubSubMessage struct {
	Message struct {
		Attributes struct {
			BuildID string `json:"buildId"`
			Status  string `json:"status"`
		} `json:"attributes"`
		Data []byte `json:"data,omitempty"`
		ID   string `json:"id"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}
