package parser

import (
	"log"
	"testing"
)

func TestSyntaxParsing(t *testing.T) {
	syntax := `

	hello() {
		toto = tata
		tata = "tutu"
		hello2 {
			tutu = tonton(concat("abc", "def"), toto)
		}
		hello2 toto (1, 2 3) {
			foo = bar
		}
	}

	f1 = "/etc/news.inn.conf"

	data file 1 {
		path = f1
	}

	resource file_line inn_conf {
		input = data.file.1
		replace {
			pattern     = "(foo)"
			replacement = "$1bar"
		}
	}

	resource file 1 {
		path    = f1
		content = inn_conf
		mode    = 0755
	}
	`
	p := NewParser(syntax)
	p.Parse()
	log.Printf("%v", p)
	for _, err := range p.Errors {
		log.Println(err)
		t.Fail()
	}
}
