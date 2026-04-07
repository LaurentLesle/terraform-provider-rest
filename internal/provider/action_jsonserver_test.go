package provider_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/LaurentLesle/terraform-provider-rest/internal/acceptance"
)

type jsonServerAction struct {
	url string
}

func (d jsonServerAction) precheck(t *testing.T) {
	if d.url == "" {
		t.Skipf("%q is not specified", RESTFUL_JSON_SERVER_URL)
	}
}

func newJsonServerAction() jsonServerAction {
	return jsonServerAction{
		url: os.Getenv(RESTFUL_JSON_SERVER_URL),
	}
}

func TestAction_JSONServer_Basic(t *testing.T) {
	d := newJsonServerAction()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { d.precheck(t) },
		ProtoV6ProviderFactories: acceptance.ProviderFactory(),
		Steps: []resource.TestStep{
			{
				Config:            d.basic(),
				ConfigStateChecks: []statecheck.StateCheck{},
			},
		},
	})
}

func (d jsonServerAction) basic() string {
	return fmt.Sprintf(`
provider "rest" {
  base_url = %q
}

resource "rest_resource" "test" {
  path = "posts"
  body = {
  	foo = "bar"
  }
  ephemeral_body = {
	x = "y"
  }
  read_path = "$(path)/$(body.id)"

  lifecycle {
    action_trigger {
      events = [after_create]
      actions = [action.rest_action.test]
    }
  }
}

action "rest_action" "test" {
  config {
    path = rest_resource.test.id
    method = "GET"
  }
}
`, d.url)
}
