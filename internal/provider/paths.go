package provider

import "github.com/hashicorp/terraform-plugin-framework/path"

func pathHost() path.Path { return path.Root("host") }

func pathToken() path.Path { return path.Root("token") }
