// Mgmt
// Copyright (C) 2013-2016+ James Shubin and the project contributors
// Written by James Shubin <james@shubin.ca> and the project contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package newgraph

import (
	"fmt"
	"log"
	"sync"

	"github.com/purpleidea/mgmt/gapi"
	"github.com/purpleidea/mgmt/pgraph"
	"github.com/purpleidea/mgmt/recwatch"
)

// GAPI implements the main yamlgraph GAPI interface.
type GAPI struct {
	File *string // yaml graph definition to use; nil if undefined

	data        gapi.Data
	initialized bool
	closeChan   chan struct{}
	wg          sync.WaitGroup // sync group for tunnel go routines
}

// NewGAPI creates a new newgraph GAPI struct and calls Init().
func NewGAPI(data gapi.Data, file *string) (*GAPI, error) {
	obj := &GAPI{
		File: file,
	}
	return obj, obj.Init(data)
}

// Init initializes the newgraph GAPI struct.
func (obj *GAPI) Init(data gapi.Data) error {
	if obj.initialized {
		return fmt.Errorf("Already initialized!")
	}
	if obj.File == nil {
		return fmt.Errorf("The File param must be specified!")
	}
	obj.data = data // store for later
	obj.closeChan = make(chan struct{})
	obj.initialized = true
	return nil
}

// Graph returns a current Graph.
func (obj *GAPI) Graph() (*pgraph.Graph, error) {
	if !obj.initialized {
		return nil, fmt.Errorf("newgraph: GAPI is not initialized")
	}

	config := ParseConfigFromFile(*obj.File)
	if config == nil {
		return nil, fmt.Errorf("newgraph: ParseConfigFromFile returned nil")
	}

	g, err := config.NewGraphFromConfig(obj.data.Hostname, obj.data.World, obj.data.Noop)
	return g, err
}

// Next returns nil errors every time there could be a new graph.
func (obj *GAPI) Next() chan error {
	if obj.data.NoWatch {
		return nil
	}
	ch := make(chan error)
	obj.wg.Add(1)
	go func() {
		defer obj.wg.Done()
		defer close(ch) // this will run before the obj.wg.Done()
		if !obj.initialized {
			ch <- fmt.Errorf("newgraph: GAPI is not initialized")
			return
		}
		configWatcher := recwatch.NewConfigWatcher()
		configWatcher.Add(*obj.File)
		for {
			select {
			case _, ok := <-configWatcher.Events():
				if !ok { // the channel closed!
					return
				}
				log.Printf("newgraph: Generating new graph...")
				select {
				case ch <- nil: // trigger a run (send a msg)
				// unblock if we exit while waiting to send!
				case <-obj.closeChan:
					return
				}
			case err, ok := <-configWatcher.Error():
				if !ok { // the channel closed!
					return
				}
				log.Printf("newgraph: Error %s", err.Error())
				select {
				case ch <- err: // trigger a run (send a msg)
					return
				// unblock if we exit while waiting to send!
				case <-obj.closeChan:
					return
				}
			case <-obj.closeChan:
				return
			}
		}
	}()
	return ch
}

// Close shuts down the yamlgraph GAPI.
func (obj *GAPI) Close() error {
	if !obj.initialized {
		return fmt.Errorf("newgraph: GAPI is not initialized")
	}
	close(obj.closeChan)
	obj.wg.Wait()
	obj.initialized = false // closed = true
	return nil
}
