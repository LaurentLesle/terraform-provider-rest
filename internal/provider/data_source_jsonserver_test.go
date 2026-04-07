package provider_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/LaurentLesle/terraform-provider-rest/internal/acceptance"
)

func TestDataSource_JSONServer_Basic(t *testing.T) {
	addr := "rest_resource.test"
	dsaddr := "data.rest_resource.test"
	d := newJsonServerData()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { d.precheck(t) },
		CheckDestroy:             d.CheckDestroy(addr),
		ProtoV6ProviderFactories: acceptance.ProviderFactory(),
		Steps: []resource.TestStep{
			{
				Config: d.dsBasic(),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(dsaddr, tfjsonpath.New("output").AtMapKey("id"), knownvalue.NotNull()),
				},
			},
		},
	})
}

func TestDataSource_JSONServer_WithSelector(t *testing.T) {
	addr := "rest_resource.test"
	dsaddr := "data.rest_resource.test"
	d := newJsonServerData()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { d.precheck(t) },
		CheckDestroy:             d.CheckDestroy(addr),
		ProtoV6ProviderFactories: acceptance.ProviderFactory(),
		Steps: []resource.TestStep{
			{
				Config: d.dsWithSelector(),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(dsaddr, tfjsonpath.New("output").AtMapKey("id"), knownvalue.NotNull()),
				},
			},
		},
	})
}

func TestDataSource_JSONServer_WithOutputAttrs(t *testing.T) {
	addr := "rest_resource.test"
	dsaddr := "data.rest_resource.test"
	d := newJsonServerData()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { d.precheck(t) },
		CheckDestroy:             d.CheckDestroy(addr),
		ProtoV6ProviderFactories: acceptance.ProviderFactory(),
		Steps: []resource.TestStep{
			{
				Config: d.dsWithOutputAttrs(),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(dsaddr, tfjsonpath.New("output").AtMapKey("foo"), knownvalue.StringExact("bar")),
					statecheck.ExpectKnownValue(dsaddr, tfjsonpath.New("output").AtMapKey("obj").AtMapKey("a"), knownvalue.NumberExact(big.NewFloat(1))),
				},
			},
		},
	})
}

func TestDataSource_JSONServer_NotExists(t *testing.T) {
	dsaddr := "data.rest_resource.test"
	d := newJsonServerData()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { d.precheck(t) },
		ProtoV6ProviderFactories: acceptance.ProviderFactory(),
		Steps: []resource.TestStep{
			{
				Config: d.dsNotExist(),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(dsaddr, tfjsonpath.New("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(dsaddr, tfjsonpath.New("output"), knownvalue.Null()),
				},
			},
		},
	})
}

func (d jsonServerData) dsBasic() string {
	return fmt.Sprintf(`
provider "rest" {
  base_url = %q
}

resource "rest_resource" "test" {
  path = "/posts"
  body = {
  	foo = "bar"
  }
  read_path = "$(path)/$(body.id)"
}

data "rest_resource" "test" {
  id = rest_resource.test.id
}
`, d.url)

}

func (d jsonServerData) dsWithSelector() string {
	return fmt.Sprintf(`
provider "rest" {
  base_url = %q
}

resource "rest_resource" "test" {
  path = "/posts"
  body = {
  	foo = "bar"
  }
  read_path = "$(path)/$(body.id)"
}

data "rest_resource" "test" {
  id       = "/posts"
  selector = "#(foo==\"bar\")"
  depends_on = [rest_resource.test]
}
`, d.url)

}

func (d jsonServerData) dsWithOutputAttrs() string {
	return fmt.Sprintf(`
provider "rest" {
  base_url = %q
}

resource "rest_resource" "test" {
  path = "/posts"
  body = {
  	foo = "bar"
	obj = {
	  a = 1
	  b = 2
	}
  }
  read_path = "$(path)/$(body.id)"
}

data "rest_resource" "test" {
  id = rest_resource.test.id
  output_attrs = ["foo", "obj.a"]
}
`, d.url)

}

func (d jsonServerData) dsNotExist() string {
	return fmt.Sprintf(`
provider "rest" {
  base_url = %q
}

data "rest_resource" "test" {
  id = "/notexist"
  allow_not_exist = true
}
`, d.url)

}

func TestDataSource_JSONServer_UseSensitiveOutput(t *testing.T) {
	dsaddr := "data.rest_resource.test"
	d := newJsonServerData()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { d.precheck(t) },
		ProtoV6ProviderFactories: acceptance.ProviderFactory(),
		Steps: []resource.TestStep{
			{
				Config: d.dsUseSensitiveOutput(),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(dsaddr, tfjsonpath.New("sensitive_output").AtMapKey("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(dsaddr, tfjsonpath.New("sensitive_output").AtMapKey("foo"), knownvalue.StringExact("bar")),
					statecheck.ExpectKnownValue(dsaddr, tfjsonpath.New("output"), knownvalue.Null()),
				},
			},
		},
	})
}

func (d jsonServerData) dsUseSensitiveOutput() string {
	return fmt.Sprintf(`
provider "rest" {
  base_url = %q
}

resource "rest_resource" "test" {
  path = "/posts"
  body = {
  	foo = "bar"
  }
  read_path = "$(path)/$(body.id)"
}

data "rest_resource" "test" {
  id = rest_resource.test.id
  use_sensitive_output = true
}
`, d.url)

}
