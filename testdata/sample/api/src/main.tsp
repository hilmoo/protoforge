import "@typespec/http";
import "@typespec/openapi";
import "@typespec/openapi3";

import "./authors.tsp";
import "./books.tsp";

using TypeSpec.Http;
using TypeSpec.OpenAPI;

@service(#{ title: "Bookstore" })
@info(#{ version: "1.0.0" })
@doc("protoforge sample service")
@server("/", "Default server")
namespace Bookstore;
