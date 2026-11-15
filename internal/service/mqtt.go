package service

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strconv"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

type MQTTConnection interface {
	Publish(context.Context, string, []byte) error
	Done() <-chan struct{}
	Close(context.Context) error
}
type MQTTOptions struct {
	InstanceID, BaseTopic, DeviceName, DiscoveryPrefix, SoftwareVersion string
	IncludeReeve, AttentionDetails                                      bool
}
type mqttAgents interface {
	ListAgents(context.Context) ([]AgentSession, error)
}
type mqttAttention interface {
	List(context.Context, AttentionFilter) ([]AttentionItem, error)
}
type mqttApprovals interface {
	List(context.Context, store.ApprovalFilter) ([]Approval, error)
}

type MQTTPublisher struct {
	opts      MQTTOptions
	agents    mqttAgents
	attention mqttAttention
	approvals mqttApprovals
	bus       *events.Bus
	dial      func(context.Context) (MQTTConnection, error)
	birth     chan struct{}
}

func NewMQTTPublisher(opts MQTTOptions, agents mqttAgents, attention mqttAttention, approvals mqttApprovals, bus *events.Bus, dial func(context.Context) (MQTTConnection, error)) *MQTTPublisher {
	return &MQTTPublisher{opts: opts, agents: agents, attention: attention, approvals: approvals, bus: bus, dial: dial, birth: make(chan struct{}, 1)}
}

// HomeAssistantOnline requests a fresh discovery and authoritative snapshot.
func (p *MQTTPublisher) HomeAssistantOnline() {
	select {
	case p.birth <- struct{}{}:
	default:
	}
}

// Run owns one connection at a time. It never queues historical snapshots.
func (p *MQTTPublisher) Run(ctx context.Context) {
	changes, cancel := p.bus.SubscribeInvalidations(events.EventAgentStarted, events.EventAgentActivityChanged, events.EventAgentCompleted, events.EventAgentDied, events.EventAttentionChanged, events.EventApprovalChanged, events.EventMQTTInvalidated)
	defer cancel()
	backoff := time.Second
	for ctx.Err() == nil {
		connectCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		conn, err := p.dial(connectCtx)
		stop()
		if err != nil {
			log.Print("MQTT: connection failed; check broker and credential configuration")
		} else {
			backoff = time.Second
			p.connected(ctx, conn, changes)
			shutdownCtx, done := context.WithTimeout(context.Background(), 2*time.Second)
			if err := conn.Publish(shutdownCtx, p.opts.BaseTopic+"/availability", []byte("offline")); err != nil {
				log.Print("MQTT: offline publication failed")
			}
			_ = conn.Close(shutdownCtx)
			done()
		}
		if !mqttDelay(ctx, backoff) {
			return
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}
func mqttDelay(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (p *MQTTPublisher) connected(ctx context.Context, conn MQTTConnection, changes <-chan struct{}) {
	previous := map[string]string{}
	full := true
	backoff := time.Second
	for {
		payloads, err := p.snapshot(ctx)
		if err != nil {
			log.Print("MQTT: source snapshot failed; availability is offline")
			publishCtx, stop := context.WithTimeout(ctx, 2*time.Second)
			e := conn.Publish(publishCtx, p.opts.BaseTopic+"/availability", []byte("offline"))
			stop()
			if e != nil || !mqttDelay(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, 30*time.Second)
			full = true
			continue
		}
		backoff = time.Second
		if full {
			for topic, payload := range p.discovery() {
				payloads[topic] = payload
			}
		}
		for topic, payload := range payloads {
			if !full && previous[topic] == payload {
				continue
			}
			publishCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			err = conn.Publish(publishCtx, topic, []byte(payload))
			stop()
			if err != nil {
				log.Print("MQTT: publication failed; reconnecting")
				return
			}
			previous[topic] = payload
		}
		if full {
			publishCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			err = conn.Publish(publishCtx, p.opts.BaseTopic+"/availability", []byte("online"))
			stop()
			if err != nil {
				log.Print("MQTT: online publication failed; reconnecting")
				return
			}
		}
		full = false
		select {
		case <-ctx.Done():
			return
		case <-conn.Done():
			return
		case <-p.birth:
			full = true
		case <-changes:
			// Fixed window: continuous changes cannot postpone publication indefinitely.
			if !mqttDelay(ctx, 100*time.Millisecond) {
				return
			}
			select {
			case <-changes:
			default:
			}
		}
	}
}

type mqttEntity struct {
	key, name string
	binary    bool
}

var mqttEntities = []mqttEntity{
	{"agents_running", "Agents running", false}, {"agents_working", "Agents working", false}, {"agents_waiting", "Agents waiting", false}, {"agents_idle", "Agents idle", false}, {"agents_background", "Agents background", false}, {"agents_error", "Agents error", false},
	{"needs_attention", "Needs attention", true}, {"attention_items", "Attention items", false}, {"agent_waiting_on_you", "Agent waiting on you", true}, {"pending_approvals", "Pending approvals", false},
}

func (p *MQTTPublisher) discovery() map[string]string {
	out := map[string]string{}
	for _, entity := range mqttEntities {
		component := "sensor"
		if entity.binary {
			component = "binary_sensor"
		}
		payload := map[string]any{"name": entity.name, "unique_id": p.opts.InstanceID + "_" + entity.key, "state_topic": p.opts.BaseTopic + "/" + entity.key + "/state", "availability_topic": p.opts.BaseTopic + "/availability", "device": map[string]any{"identifiers": []string{p.opts.InstanceID}, "name": p.opts.DeviceName, "manufacturer": "Legato", "model": "Legato", "sw_version": p.opts.SoftwareVersion}}
		if entity.binary {
			payload["payload_on"] = "ON"
			payload["payload_off"] = "OFF"
		}
		if entity.key == "attention_items" && p.opts.AttentionDetails {
			payload["json_attributes_topic"] = p.opts.BaseTopic + "/attention_items/attributes"
		}
		data, _ := json.Marshal(payload)
		out[p.opts.DiscoveryPrefix+"/"+component+"/"+p.opts.InstanceID+"/"+entity.key+"/config"] = string(data)
	}
	return out
}
func (p *MQTTPublisher) snapshot(ctx context.Context) (map[string]string, error) {
	agents, err := p.agents.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	items, err := p.attention.List(ctx, AttentionFilter{})
	if err != nil {
		return nil, err
	}
	approvals, err := p.approvals.List(ctx, store.ApprovalFilter{Status: "pending"})
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, agent := range agents {
		if agent.Status != "running" || (agent.Reeve && !p.opts.IncludeReeve) {
			continue
		}
		counts["agents_running"]++
		activity := agent.Activity
		if activity == "" {
			activity = "idle"
		}
		counts["agents_"+activity]++
	}
	counts["attention_items"] = len(items)
	counts["pending_approvals"] = len(approvals)
	out := map[string]string{}
	for _, entity := range mqttEntities {
		value := strconv.Itoa(counts[entity.key])
		if entity.binary {
			value = "OFF"
			if (entity.key == "needs_attention" && len(items) > 0) || (entity.key == "agent_waiting_on_you" && counts["agents_waiting"] > 0) {
				value = "ON"
			}
		}
		out[p.opts.BaseTopic+"/"+entity.key+"/state"] = value
	}
	out[p.opts.BaseTopic+"/attention_items/attributes"] = ""
	if p.opts.AttentionDetails {
		details := make([]map[string]string, 0, len(items))
		for _, item := range items {
			kind, label := mqttSafeSignal(item.SignalKind)
			details = append(details, map[string]string{"kind": kind, "title": label})
		}
		sort.Slice(details, func(i, j int) bool { return details[i]["kind"] < details[j]["kind"] })
		if len(details) > 20 {
			details = details[:20]
		}
		data, _ := json.Marshal(map[string]any{"items": details, "total": len(items), "truncated": len(items) > len(details)})
		out[p.opts.BaseTopic+"/attention_items/attributes"] = string(data)
	}
	return out, nil
}
func mqttSafeSignal(kind string) (string, string) {
	switch kind {
	case AttentionSignalAgentWaiting:
		return kind, "Agent needs input"
	case AttentionSignalAgentError:
		return kind, "Agent error"
	case AttentionSignalApproval:
		return kind, "Approval pending"
	case AttentionSignalPlanDecision:
		return kind, "Plan decision pending"
	case AttentionSignalReviewReady:
		return kind, "Review ready"
	case AttentionSignalPRMergeBlocked:
		return kind, "Pull request blocked"
	case AttentionSignalWorkflowFailed:
		return kind, "Workflow failed"
	default:
		return "attention", "Attention required"
	}
}
