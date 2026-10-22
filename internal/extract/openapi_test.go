package extract

import (
	"reflect"
	"testing"
)

const spec = `openapi: 3.0.3
security:
  - bearerAuth: []
paths:
  /things/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema: {type: string, format: uuid}
    get:
      operationId: get_thing
      summary: Fetch one
      tags: [things]
      security: []
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Thing"
        "404":
          $ref: "#/components/responses/NotFound"
    put:
      operationId: putThing
      x-rate-limit: {scope: ip, requests: 5, window: 60s}
      requestBody:
        content:
          application/json:
            schema:
              allOf:
                - $ref: "#/components/schemas/ThingInput"
      responses:
        "204":
          description: Updated
  /things:
    get:
      operationId: listThings
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: "#/components/schemas/Thing"
components:
  responses:
    NotFound:
      description: Nothing there
      content:
        application/json:
          schema:
            $ref: "#/components/schemas/Error"
  schemas:
    Thing:
      type: object
      description: A thing.
      required: [id]
      properties:
        id: {type: string, format: uuid}
        parts:
          type: array
          items:
            $ref: "#/components/schemas/Part"
        parent:
          $ref: "#/components/schemas/Thing"
    Part:
      type: object
      properties:
        label: {type: string}
    ThingInput:
      type: object
      properties:
        name: {type: string}
    Error:
      type: object
`

func TestParseSpec(t *testing.T) {
	res, err := ParseSpec("things", "api/services/things.yaml", []byte(spec))
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]int{}
	for i, o := range res.Operations {
		ops[o.ID] = i
	}
	if len(ops) != 3 {
		t.Fatalf("operations: %+v", res.Operations)
	}
	get := res.Operations[ops["get_thing"]]
	if get.Method != "GET" || get.Path != "/things/{id}" || get.Owner != "things" || get.Summary != "Fetch one" {
		t.Errorf("get: %+v", get)
	}
	if len(get.Security) != 0 {
		t.Errorf("explicit security: [] must make the operation public, got %v", get.Security)
	}
	if len(get.Parameters) != 1 || get.Parameters[0].Name != "id" || !get.Parameters[0].Required || get.Parameters[0].Type != "string" {
		t.Errorf("path-level parameters: %+v", get.Parameters)
	}
	if len(get.Responses) != 2 || get.Responses[0].Status != "200" ||
		!reflect.DeepEqual(get.Responses[0].Schemas, []string{"things.Thing"}) ||
		get.Responses[1].Description != "Nothing there" ||
		!reflect.DeepEqual(get.Responses[1].Schemas, []string{"things.Error"}) {
		t.Errorf("responses: %+v", get.Responses)
	}
	if get.Line != 11 {
		t.Errorf("line = %d", get.Line)
	}
	put := res.Operations[ops["putThing"]]
	if !reflect.DeepEqual(put.Security, []string{"bearerAuth"}) {
		t.Errorf("global security should be inherited: %v", put.Security)
	}
	if put.RateLimit["scope"] != "ip" || put.RateLimit["requests"] != 5 {
		t.Errorf("rate limit: %v", put.RateLimit)
	}
	if !reflect.DeepEqual(put.RequestSchemas, []string{"things.ThingInput"}) {
		t.Errorf("request schemas through allOf: %v", put.RequestSchemas)
	}
	list := res.Operations[ops["listThings"]]
	if !reflect.DeepEqual(list.Responses[0].Schemas, []string{"things.Thing"}) {
		t.Errorf("refs through array items: %v", list.Responses[0].Schemas)
	}

	sc := map[string][]string{}
	for _, s := range res.Schemas {
		sc[s.ID] = s.Refs
	}
	// Thing references Part through an array property; its self reference is dropped.
	if !reflect.DeepEqual(sc["things.Thing"], []string{"things.Part"}) {
		t.Errorf("Thing refs: %v", sc["things.Thing"])
	}
	for _, s := range res.Schemas {
		if s.ID == "things.Thing" {
			if len(s.Properties) != 3 || s.Properties[1].Type != "[]Part" || !s.Properties[0].Required || s.Properties[0].Format != "uuid" || s.Properties[2].Type != "Thing" {
				t.Errorf("properties: %+v", s.Properties)
			}
		}
	}
}

func TestGoName(t *testing.T) {
	for in, want := range map[string]string{
		"listItems": "ListItems", "get_item": "GetItem", "getHealth": "GetHealth", "x-y-z": "XYZ", "A": "A",
	} {
		if got := GoName(in); got != want {
			t.Errorf("GoName(%q) = %q, want %q", in, got, want)
		}
	}
}
