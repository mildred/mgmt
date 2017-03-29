package parser

import (
	"fmt"
)

type Parser struct {
	n      int
	l      int
	c      int
	str    string
	Root   *Resource
	Errors []Error
}

func NewParser(content string) *Parser {
	r := &Resource{}
	r.init()
	return &Parser{
		str:  content,
		Root: r,
	}
}

type Location struct {
	Offset int
	Line   int
	Column int
	Size   int
}

type Error struct {
	Location
	Message string
}

func (err *Error) Error() string {
	return err.String()
}

func (err *Error) String() string {
	return fmt.Sprintf(":%d:%d: %s", err.Line, err.Column, err.Message)
}

type Resource struct {
	Location
	Name        string
	Descriptors []*Expr
	Attributes  []*Expr
	Bindings    map[string]*Binding
	Resources   []*Resource
}

type Binding struct {
	Location
	Name string
	Expr *Expr
}

type Expr struct {
	Location
	Type     ExprType
	String   string
	Args     []*Expr
	Bindings map[string]*Binding
}

type ExprType int

const (
	ExprString ExprType = iota
	ExprVariable
	ExprFuncCall
	ExprChain
	ExprArray
	ExprObject
)

func (res *Resource) init() {
	if res.Bindings == nil {
		res.Bindings = map[string]*Binding{}
	}
}

//++ file          -> res_content
func (p *Parser) Parse() {
	p.parseResourceContent(p.Root, p.Root.Bindings)
}

//++ res_content   -> { binding | resource }
//++ binding       -> identifier ('+=' | '=') expr [ ',' ]
//++ obj_content   -> { binding }
//++ resource      -> identifier { literal } [ expr_list ] '{' res_content '}'
func (p *Parser) parseResourceContent(res *Resource, bindings map[string]*Binding) {
loop:
	for {
		var id string
		p.parseSpaces()
		p.debug("res_content")
		loc := p.location()
		c := p.get(0)
		switch c {
		case '}', 0:
			return
		default:
			id = p.parseIdentifier()
		}
		if len(id) == 0 {
			p.addError(loc, "Expected identifier")
			return
		}
		p.locationEnd(&loc)
		p.parseSpaces()
		c = p.get(0)
		switch c {
		case 0:
			return
		case '+':
			oploc := p.location()
			if p.get(1) == '=' {
				p.incr(2)
			} else {
				p.addError(oploc, "Incorrect binding operator '+'")
				p.incr(1)
			}
			p.debug("expr(+=)")
			expr := p.parseExpr(false)
			if expr == nil {
				p.addError(p.location(), "Expected expression")
				continue
			}
			b, ok := bindings[id]
			if !ok {
				b := &Binding{
					Location: loc,
					Name:     id,
					Expr: &Expr{
						Location: oploc,
						Type:     ExprArray,
						Args:     []*Expr{expr},
					},
				}
				bindings[id] = b
			} else if b.Expr.Type != ExprArray {
				b.Expr = &Expr{
					Location: oploc,
					Type:     ExprArray,
					Args:     []*Expr{b.Expr, expr},
				}
			} else {
				b.Expr.Args = append(b.Expr.Args, expr)
			}
			p.parseSpaces()
			if p.get(0) == ',' {
				p.incr(1)
			}

		case '=':
			p.incr(1)
			p.debug("expr(=)")
			expr := p.parseExpr(false)
			if expr == nil {
				p.addError(p.location(), "Expected expression")
				continue
			}
			p.parseSpaces()
			if p.get(0) == ',' {
				p.incr(1)
			}
			b := &Binding{
				Location: loc,
				Name:     id,
				Expr:     expr,
			}
			bindings[id] = b
		default:
			if res == nil {
				p.addError(p.location(), "Expected operator after identifier for binding")
				p.addError(loc, "identifier is here")
				continue loop
			}
			r := &Resource{
				Name:     id,
				Location: loc,
			}
			r.init()
			res.Resources = append(res.Resources, r)
			for {
				p.debug("expr(resource literal)")
				lit := p.parseExpr(true)
				if lit == nil {
					break
				}
				res.Descriptors = append(res.Descriptors, lit)
			}
			if p.get(0) == '(' {
				r.Attributes = p.parseExprList()
				p.locationEnd(&r.Location)
				p.parseSpaces()
			}
			if p.get(0) == '{' {
				p.incr(1)
				p.parseResourceContent(r, r.Bindings)
				p.locationEnd(&r.Location)
				p.parseSpaces()
				if p.get(0) == '}' {
					p.locationEnd(&r.Location)
					p.incr(1)
				}
			} else {
				p.addError(p.location(), "Expected { to start resource")
				p.addError(loc, "Resource starts here")
			}
		}
	}
}

//++ expr          -> literal
//++                | function_call
//++                | '{' obj_content '}'
//++                | expr_array
//++                | expr '.' identifier
//++ literal       -> string
//++                | variable
//++ variable      -> identifier
//++ function_call -> identifier expr_list
func (p *Parser) parseExpr(literal bool) *Expr {
	var e *Expr
	p.parseSpaces()
	p.debug("expr")
	loc := p.location()
	c := p.get(0)
	switch c {
	case 0, ',', ')':
		return nil
	case '"':
		str := p.parseString()
		e = &Expr{
			Location: loc,
			Type:     ExprString,
			String:   str,
		}
		p.locationEnd(&e.Location)
		if literal {
			return e
		}
	case '[':
		if literal {
			return nil
		}
		e = &Expr{
			Location: loc,
			Type:     ExprArray,
			Args:     p.parseExprList(),
		}
		p.locationEnd(&e.Location)
	case '{':
		if literal {
			return nil
		}
		p.incr(1)
		e = &Expr{
			Location: loc,
			Type:     ExprObject,
			Bindings: map[string]*Binding{},
		}
		p.parseResourceContent(nil, e.Bindings)
		p.parseSpaces()
		p.locationEnd(&e.Location)
		if p.get(0) == '}' {
			p.incr(1)
		}
	default:
		id := p.parseIdentifier()
		p.locationEnd(&loc)
		if len(id) == 0 {
			return nil
		}
		e = &Expr{
			Location: loc,
			Type:     ExprVariable,
			String:   id,
		}
		if literal {
			return e
		}
		p.parseSpaces()
		p.debug("expr after identifier")
		c := p.get(0)
		switch c {
		default:
			e = &Expr{
				Location: loc,
				Type:     ExprVariable,
				String:   id,
			}
		case '(':
			e = &Expr{
				Location: loc,
				Type:     ExprFuncCall,
				String:   id,
				Args:     p.parseExprList(),
			}
			p.locationEnd(&e.Location)
		}
	}
loop:
	for {
		p.parseSpaces()
		c := p.get(0)
		switch c {
		default:
			break loop
		case '.':
			p.incr(1)
			p.parseSpaces()
			l := p.location()
			id := p.parseIdentifier()
			if id == "" {
				p.addError(p.location(), "Expected chain identifier")
			} else {
				p.locationEnd(&l)
				if e.Type != ExprChain {
					e = &Expr{
						Location: loc,
						Type:     ExprChain,
						Args:     []*Expr{e},
					}
				}
				e.Args = append(e.Args, &Expr{
					Location: l,
					Type:     ExprVariable,
					String:   id,
				})
				p.locationEnd(&e.Location)
			}
		}
	}
	return e
}

//++ expr_list     -> '(' { expr [ ',' ] } ')'
//++ expr_array    -> '[' { expr [ ',' ] } ']'
func (p *Parser) parseExprList() []*Expr {
	var res []*Expr
	var end byte
	switch p.get(0) {
	case '(':
		end = ')'
	case '[':
		end = ']'
	default:
		return nil
	}
	p.debug("expr_list")
	p.incr(1)
	for {
		p.debug("expr(list)")
		e := p.parseExpr(false)
		if e != nil {
			res = append(res, e)
		}

		p.parseSpaces()
		c := p.get(0)
		switch c {
		case end:
			p.incr(1)
			fallthrough
		case 0:
			p.debug("expr_list end")
			return res
		case ',':
			p.incr(1)
		default:
		}

		if e == nil {
			p.addError(p.location(), "Expected expression in list")
			p.debug("expr_list end forced")
			return res
		}
	}
}

//++ string        -> '"' { ^'"' } '"'
func (p *Parser) parseString() string {
	var res string
	if p.get(0) != '"' {
		p.addError(p.location(), "Expected string")
		return ""
	}
	p.incr(1)
	p.debug("string")
	for {
		c := p.get(0)
		switch c {
		default:
			res += string(c)
		case '"':
			p.incr(1)
			fallthrough
		case 0:
			return res
		}
		p.incr(1)
	}
}

//++ identifier    -> { ^( ' ' | '\t' | '\n' | '\r' |
//++                       '{' | '}'  | '('  | ')'  |
//++                       '[' | ']'  | '+'
//++                       '=' | ','  | '"'  | '.'  ) }
func (p *Parser) parseIdentifier() string {
	var res string
	for {
		//p.debug("identifier")
		c := p.get(0)
		switch c {
		default:
			res += string(c)
		case 0, ' ', '\t', '\n', '\r', '{', '}', '(', ')', '[', ']', '+', '=', ',', '"', '.':
			return res
		}
		p.incr(1)
	}
}

//++ spaces        -> { ' ' | '\t' | '\n' | '\r' }
func (p *Parser) parseSpaces() {
	for {
		//p.debug("space")
		c := p.get(0)
		switch c {
		default:
			return
		case ' ', '\t', '\n', '\r':
			p.incr(1)
		}
	}
}

func (p *Parser) locationEnd(loc *Location) {
	loc.Size = p.n - loc.Offset
}

func (p *Parser) location() Location {
	return Location{
		Offset: p.n,
		Line:   p.l + 1,
		Column: p.c,
		Size:   0,
	}
}

func (p *Parser) get(i int) byte {
	if p.n+i < len(p.str) {
		return p.str[p.n+i]
	} else {
		return 0
	}
}

func (p *Parser) incr(i int) {
	if i == 0 || p.n >= len(p.str) {
		return
	}
	if p.str[p.n] == '\n' {
		p.l++
		p.c = 0
	}
	p.n++
	p.incr(i - 1)
}

func (p *Parser) addError(loc Location, msg string) {
	p.debug("error: " + msg)
	p.Errors = append(p.Errors, Error{loc, msg})
}

func (p *Parser) debug(msg string) {
	/*
		var before, after string
		if p.n > 0 {
			i := p.n - 5
			if i < 0 {
				i = 0
			}
			before = p.str[i:p.n]
		}
		if p.n < len(p.str) {
			i := p.n + 5
			if i > len(p.str) {
				i = len(p.str)
			}
			after = p.str[p.n:i]
		}
		fmt.Printf("%d:%d:%d: %#v-%#v %s\n", p.l+1, p.c, p.n, before, after, msg)
		//*/
}
