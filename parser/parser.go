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

func (err *Error) String() string {
	return fmt.Sprintf(":%d:%d: %d", err.Line, err.Column)
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
	Type   ExprType
	String string
	Args   []*Expr
}

type ExprType int

const (
	ExprString ExprType = iota
	ExprVariable
	ExprFuncCall
	ExprChain
)

func (res *Resource) init() {
	if res.Bindings == nil {
		res.Bindings = map[string]*Binding{}
	}
}

//++ file          -> res_content
func (p *Parser) Parse() {
	p.parseResourceContent(p.Root)
}

//++ res_content   -> { binding | resource }
//++ binding       -> identifier '=' expr
//++ resource      -> identifier { literal } [ expr_list ] '{' res_content '}'
func (p *Parser) parseResourceContent(res *Resource) {
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
		case '=':
			p.incr(1)
			expr := p.parseExpr(false)
			if expr == nil {
				p.addError(p.location(), "Expected expression")
				continue
			}
			b := &Binding{
				Location: loc,
				Name:     id,
				Expr:     expr,
			}
			res.Bindings[id] = b
		default:
			r := &Resource{
				Name:     id,
				Location: loc,
			}
			r.init()
			res.Resources = append(res.Resources, r)
			for {
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
				p.parseResourceContent(r)
				p.locationEnd(&r.Location)
				p.parseSpaces()
				if p.get(0) == '}' {
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
//++                | expr '.' identifier
//++ literal       -> string | variable
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
func (p *Parser) parseExprList() []*Expr {
	var res []*Expr
	p.debug("expr_list")
	if p.get(0) != '(' {
		return nil
	}
	p.incr(1)
	for {
		e := p.parseExpr(false)
		if e != nil {
			res = append(res, e)
		}

		p.parseSpaces()
		c := p.get(0)
		switch c {
		case ')':
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
//++                       '=' | ','  | '"'  | '.'  ) }
func (p *Parser) parseIdentifier() string {
	var res string
	for {
		//p.debug("identifier")
		c := p.get(0)
		switch c {
		default:
			res += string(c)
		case 0, ' ', '\t', '\n', '\r', '{', '}', '(', ')', '=', ',', '"', '.':
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
	*/
}
