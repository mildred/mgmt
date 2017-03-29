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

// Package newgraph provides the facilities for loading a graph from a yaml file.
package newgraph

import (
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	"github.com/purpleidea/mgmt/gapi"
	"github.com/purpleidea/mgmt/parser"
	"github.com/purpleidea/mgmt/pgraph"
	"github.com/purpleidea/mgmt/resources"
	"github.com/purpleidea/mgmt/util"

	"gopkg.in/yaml.v2"
)

// Vertex is the data structure of a vertex.
type Vertex struct {
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
}

// Edge is the data structure of an edge.
type Edge struct {
	Name   string `yaml:"name"`
	From   Vertex `yaml:"from"`
	To     Vertex `yaml:"to"`
	Notify bool   `yaml:"notify"`
}

// GraphConfig is the data structure that describes a single graph to run.
type GraphConfig struct {
	Graph   string
	Edges   []Edge `yaml:"edges"`
	ResList []resources.Res
}

func evalResource(r *parser.Resource) (interface{}, error) {
	var err error
	res := map[string]interface{}{}
	for k, v := range r.Bindings {
		res[k], err = evalExpr(v.Expr)
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

func evalExpr(expr *parser.Expr) (interface{}, error) {
	var err error
	switch expr.Type {
	case parser.ExprArray:
		var res []interface{}
		for _, item := range expr.Args {
			e, err := evalExpr(item)
			if err != nil {
				return nil, err
			}
			res = append(res, e)
		}
		return res, nil
	case parser.ExprString:
		return expr.String, nil
	case parser.ExprObject:
		res := map[string]interface{}{}
		for k, v := range expr.Bindings {
			res[k], err = evalExpr(v.Expr)
			if err != nil {
				return nil, err
			}
		}
		return res, nil
	case parser.ExprChain:
		return nil, fmt.Errorf("line %d column %d: Expression chain not supported yet", expr.Line, expr.Column)
	case parser.ExprFuncCall:
		return nil, fmt.Errorf("line %d column %d: Function %s() not supported", expr.Line, expr.Column, expr.String)
	case parser.ExprVariable:
		return nil, fmt.Errorf("line %d column %d: Variables not supported", expr.Line, expr.Column)
	default:
		panic("Unknown type")
	}
}

// Parse parses a data stream into the graph structure.
func (c *GraphConfig) Parse(res *parser.Resource) error {

	for _, r := range res.Resources {
		if r.Name != "resource" {
			continue
		}

		var kind, name string

		if len(r.Descriptors) < 1 {
			return fmt.Errorf("line %d column %d: resource kind not specified after `resource` keyword", r.Line, r.Column)
		} else {
			kind = r.Descriptors[0].String
		}

		if len(r.Descriptors) >= 2 {
			name = r.Descriptors[1].String
		}

		resource, err := resources.NewEmptyNamedResource(kind)
		if err != nil {
			return err
		}

		bindings := map[string]interface{}{}
		for k, v := range r.Bindings {
			bindings[k], err = evalExpr(v.Expr)
			if err != nil {
				return err
			}
		}

		yamlData, err := yaml.Marshal(bindings)
		if err != nil {
			return err
		}

		err = yaml.Unmarshal(yamlData, &resource)
		if err != nil {
			return err
		}

		var meta resources.MetaParams
		for _, metar := range r.Resources {
			if metar.Name != "meta" {
				continue
			}

			bindings := map[string]interface{}{}
			for k, v := range metar.Bindings {
				bindings[k], err = evalExpr(v.Expr)
				if err != nil {
					return err
				}
			}

			yamlData, err := yaml.Marshal(bindings)
			if err != nil {
				return err
			}

			err = yaml.Unmarshal(yamlData, &meta)
			if err != nil {
				return err
			}
		}

		// Set resource name, meta and kind
		resource.SetName(name)
		resource.SetKind(kind) // TODO: casing
		*resource.Meta() = meta

		c.ResList = append(c.ResList, resource)
	}
	return nil
}

// NewGraphFromConfig transforms a GraphConfig struct into a new graph.
// FIXME: remove any possibly left over, now obsolete graph diff code from here!
func (c *GraphConfig) NewGraphFromConfig(hostname string, world gapi.World, noop bool) (*pgraph.Graph, error) {
	// hostname is the uuid for the host

	var graph *pgraph.Graph          // new graph to return
	graph = pgraph.NewGraph("Graph") // give graph a default name

	var lookup = make(map[string]map[string]*pgraph.Vertex)

	//log.Printf("%+v", config) // debug

	// TODO: if defined (somehow)...
	graph.SetName(c.Graph) // set graph name

	var keep []*pgraph.Vertex        // list of vertex which are the same in new graph
	var resourceList []resources.Res // list of resources to export

	// Resources V2
	for _, res := range c.ResList {
		kind := res.Kind()
		if _, exists := lookup[kind]; !exists {
			lookup[kind] = make(map[string]*pgraph.Vertex)
		}
		// XXX: should we export based on a @@ prefix, or a metaparam
		// like exported => true || exported => (host pattern)||(other pattern?)
		if !strings.HasPrefix(res.GetName(), "@@") { // not exported resource
			v := graph.GetVertexMatch(res)
			if v == nil { // no match found
				res.Init()
				v = pgraph.NewVertex(res)
				graph.AddVertex(v) // call standalone in case not part of an edge
			}
			lookup[kind][res.GetName()] = v // used for constructing edges
			keep = append(keep, v)          // append

		} else if !noop { // do not export any resources if noop
			// store for addition to backend storage...
			res.SetName(res.GetName()[2:]) // slice off @@
			res.SetKind(kind)              // cheap init
			resourceList = append(resourceList, res)
		}
	}

	// store in backend (usually etcd)
	if err := world.ResExport(resourceList); err != nil {
		return nil, fmt.Errorf("Config: Could not export resources: %v", err)
	}

	for _, e := range c.Edges {
		if _, ok := lookup[util.FirstToUpper(e.From.Kind)]; !ok {
			return nil, fmt.Errorf("Can't find 'from' resource!")
		}
		if _, ok := lookup[util.FirstToUpper(e.To.Kind)]; !ok {
			return nil, fmt.Errorf("Can't find 'to' resource!")
		}
		if _, ok := lookup[util.FirstToUpper(e.From.Kind)][e.From.Name]; !ok {
			return nil, fmt.Errorf("Can't find 'from' name!")
		}
		if _, ok := lookup[util.FirstToUpper(e.To.Kind)][e.To.Name]; !ok {
			return nil, fmt.Errorf("Can't find 'to' name!")
		}
		from := lookup[util.FirstToUpper(e.From.Kind)][e.From.Name]
		to := lookup[util.FirstToUpper(e.To.Kind)][e.To.Name]
		edge := pgraph.NewEdge(e.Name)
		edge.Notify = e.Notify
		graph.AddEdge(from, to, edge)
	}

	return graph, nil
}

// ParseConfigFromFile takes a filename and returns the graph config structure.
func ParseConfigFromFile(filename string) *GraphConfig {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Printf("Config: Error: ParseConfigFromFile: File: %v", err)
		return nil
	}

	p := parser.NewParser(string(data))
	p.Parse()
	for _, err := range p.Errors {
		log.Printf("Config: Error: %s:%s", filename, err.Error())
	}

	var config GraphConfig
	if err := config.Parse(p.Root); err != nil {
		log.Printf("Config: Error: ParseConfigFromFile: Parse: %v", err)
		return nil
	}

	return &config
}
