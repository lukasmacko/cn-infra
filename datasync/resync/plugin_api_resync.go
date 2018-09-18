// Copyright (c) 2017 Cisco and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package resync

import (
	"github.com/ligato/cn-infra/infra"
)

// Subscriber is an API used by plugins to register for notifications from the
// RESYNC Orcherstrator.
type Subscriber interface {
	// Register function is supposed to be called in Init() by all VPP Agent plugins.
	// Those plugins will use Registration.StatusChan() to listen
	// The plugins are supposed to load current state of their objects when newResync() is called.
	Register(resyncName string) Registration
}

// Registration is an interface that is returned by the Register() call.
type Registration interface {
	StatusChan() chan StatusEvent
	String() string
	//TODO io.Closer
}

// Status used in the events.
type Status string

const (
	// Started means that the Resync has started.
	Started Status = "Started"
	// NotActive means that Resync has not started yet or it has been finished.
	NotActive = "NotActive"
)

// StatusEvent is the base type that will be propagated to the channel.
type StatusEvent interface {
	// Status() is used by the Plugin if it needs to Start resync.
	ResyncStatus() Status

	// Ack() is used by the Plugin to acknowledge that it processed this event.
	// This is supposed to be called after the configuration was applied by the Plugin.
	Ack()
}

// Reporter is an API for other plugins that need to report to RESYNC Orchestrator.
// Intent of this API is to have a chance to react on error by triggering
// RESYNC among registered plugins.
type Reporter interface {
	// ReportError is called by Plugins when the binary api call was not successful.
	// Based on that the Resync Orchestrator starts the Resync.
	ReportError(name infra.PluginName, err error)
}
