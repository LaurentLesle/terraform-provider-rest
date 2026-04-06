package provider_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/laurentlesle/terraform-provider-rest/internal/acceptance"
	"github.com/laurentlesle/terraform-provider-rest/internal/provider"
)

type jsonServerList struct {
	url string
}

func (d jsonServerList) precheck(t *testing.T) {
	if d.url == "" {
		t.Skipf("%q is not specified", RESTFUL_JSON_SERVER_URL)
	}
}

func newJsonServerList() jsonServerList {
	return jsonServerList{
		url: os.Getenv(RESTFUL_JSON_SERVER_URL),
	}
}

func TestListResource_JSONServer_Basic(t *testing.T) {
	d := newJsonServerList()
	buildIdentity := func(id string) string {
		impspec := provider.ImportSpec{
			Id:   "/posts/" + id,
			Path: "/posts",
		}
		b, _ := json.Marshal(impspec)
		return string(b)
	}

	var qchecks []querycheck.QueryResultCheck
	for i := range 3 {
		qchecks = append(qchecks,
			querycheck.ExpectIdentity("rest_resource.list", map[string]knownvalue.Check{
				"id": knownvalue.StringExact(buildIdentity(strconv.Itoa(i + 1))),
			}),
		)
	}

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		PreCheck:                 func() { d.precheck(t) },
		ProtoV6ProviderFactories: acceptance.ProviderFactory(),
		Steps: []resource.TestStep{
			// Setup
			{
				Config: d.basic(3),
			},
			{
				Query:             true,
				Config:            d.basicListQuery(),
				QueryResultChecks: qchecks,
			},
			{
				Query:  true,
				Config: d.basicListQuerySelector(1),
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectIdentity("rest_resource.list", map[string]knownvalue.Check{
						"id": knownvalue.StringExact(buildIdentity(strconv.Itoa(1))),
					}),
				},
			},
		},
	})
}

func (d jsonServerList) basic(n int) string {
	return fmt.Sprintf(`
provider "rest" {
  base_url = %q
}

resource "rest_resource" "test" {
  count = %d
  path = "posts"
  body = {
  	foo = "bar"
  }
  read_path = "$(path)/$(body.id)"
}
`, d.url, n)
}

func (d jsonServerList) basicListQuery() string {
	return `
	list "rest_resource" "list" {
		provider = restful
		config {
			path          = "/posts"
			name          = "test-$(body.id)"
			resource_path = "/posts"
			resource_id   = "$(path)/$(body.id)"
		}
	}
	`
}
func (d jsonServerList) basicListQuerySelector(n int) string {
	return fmt.Sprintf(`
	list "rest_resource" "list" {
		provider = restful
		config {
			path          = "/posts"
			name          = "test-$(body.id)"
			resource_path = "/posts"
			resource_id   = "$(path)/$(body.id)"
			selector      = "#(id==\"%d\")#"
		}
	}
	`, n)
}
