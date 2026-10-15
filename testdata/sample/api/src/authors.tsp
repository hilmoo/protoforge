import "@typespec/http";
import "@typespec/openapi";
import "./_generated/db.tsp";
import "./common/error.tsp";

using TypeSpec.Http;
using TypeSpec.OpenAPI;

namespace AuthorModel {
  @doc("An author.")
  model Author is DbSchema.AuthorRow;

  model CreateRequest is FilterVisibility<
    Author,
    #{ all: #[Lifecycle.Create] },
    "CreateRequest"
  >;

  model Id {
    @doc("The author ID.")
    @format("uuid")
    @example("4f9c2a8e-1d3b-4c5e-9f7a-2b6d8e0c1a3f")
    authorId: string;
  }
}

namespace Bookstore {
  @tag("Author")
  interface Authors {
    @doc("Create an author.")
    @post
    @route("/services.v1.AuthorService/AuthorCreate")
    create(@body body: AuthorModel.CreateRequest): AuthorModel.Author | CommonError.CommonErrors;

    @doc("Get an author.")
    @post
    @route("/services.v1.AuthorService/AuthorGet")
    get(@body body: AuthorModel.Id): AuthorModel.Author | CommonError.CommonErrors;

    @doc("List authors.")
    @post
    @route("/services.v1.AuthorService/AuthorList")
    list(): AuthorModel.Author[] | CommonError.CommonErrors;
  }
}
