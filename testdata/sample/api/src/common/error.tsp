import "@typespec/http";

using TypeSpec.Http;

namespace CommonError;

@error
@doc("An error response.")
model Error {
  @doc("Human-readable explanation.")
  @example("Book not found")
  message: string;

  @doc("Machine-readable error code.")
  @example(404)
  code: integer;

  @doc("Extra details, keyed by field.")
  details?: Record<string>;
}

@error
model Error400 extends Error {
  @statusCode
  code: 400;
}

@error
model Error404 extends Error {
  @statusCode
  code: 404;
}

alias CommonErrors = Error400 | Error404;
