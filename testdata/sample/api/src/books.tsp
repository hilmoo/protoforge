import "@typespec/http";
import "@typespec/openapi";
import "./_generated/db.tsp";
import "./common/error.tsp";
import "./common/connect.tsp";

using TypeSpec.Http;
using TypeSpec.OpenAPI;

namespace BookModel {
  @doc("A book.")
  model Book is DbSchema.BookRow;

  model CreateRequest is FilterVisibility<
    Book,
    #{ all: #[Lifecycle.Create] },
    "CreateRequest"
  >;

  model Id {
    @doc("The book ID.")
    @format("uuid")
    bookId: string;
  }

  @doc("Search books. Exposed to MCP tools.")
  @extension("x-mcp", true)
  model SearchRequest {
    @doc("Full-text query.")
    @minLength(2)
    @maxLength(200)
    @example("left hand")
    query: string;

    @doc("Restrict to one genre.")
    genre?: DbSchema.BookGenre;

    @doc("Maximum number of results.")
    @minValue(1)
    @maxValue(100)
    limit?: int32 = 20;

    @doc("Only books published after this date.")
    publishedAfter?: plainDate;

    @doc("Preferred delivery time.")
    deliverAt?: plainTime;

    @doc("ISBN filter.")
    @pattern("^[0-9-]{10,17}$")
    @extension("x-validator", "isbn")
    isbn?: string;

    @doc("Required tags.")
    @minItems(1)
    @maxItems(5)
    tags?: string[];

    @doc("Include out-of-print books.")
    includeOutOfPrint: boolean;

    @doc("Sort order.")
    sort?: "relevance" | "newest" | "price";
  }

  @doc("A page of search results.")
  model SearchResponse {
    @doc("Matching books.")
    items: Book[];

    @doc("Total number of matches.")
    @minValue(0)
    total: int64;
  }

  model ReviewInput {
    @doc("Rating from 1 to 5.")
    @minValue(1)
    @maxValue(5)
    rating: int32;

    @doc("Review text.")
    @maxLength(5000)
    body?: string;
  }

  model BulkReviewRequest {
    @doc("The book being reviewed.")
    @format("uuid")
    bookId: string;

    @doc("Reviews to add.")
    reviews: ReviewInput[];
  }

  @doc("A review.")
  model Review is DbSchema.ReviewRow;
}

namespace Bookstore {
  @tag("Book")
  interface Books {
    @doc("Create a book.")
    @post
    @route("/services.v1.BookService/BookCreate")
    create(@body body: BookModel.CreateRequest): BookModel.Book | CommonError.CommonErrors;

    @doc("Get a book.")
    @post
    @route("/services.v1.BookService/BookGet")
    get(@body body: BookModel.Id): BookModel.Book | CommonError.CommonErrors;

    @doc("Search books.")
    @post
    @route("/services.v1.BookService/BookSearch")
    search(@body body: BookModel.SearchRequest): BookModel.SearchResponse | CommonError.CommonErrors;

    @doc("Add several reviews at once.")
    @post
    @route("/services.v1.BookService/BookBulkReview")
    bulkReview(@body body: BookModel.BulkReviewRequest): BookModel.Review[] | CommonError.CommonErrors;

    @doc("Delete a book.")
    @post
    @route("/services.v1.BookService/BookDelete")
    delete(@body body: BookModel.Id): CommonConnect.Empty | CommonError.CommonErrors;

    @doc("Get a book cover by ISBN (REST style, with path and query parameters).")
    @get
    @route("/covers/{isbn}")
    cover(
      @doc("The ISBN.") @path isbn: string,
      @doc("Cover width in pixels.") @query @minValue(16) @maxValue(2048) width?: int32,
      @doc("Image format.") @query format?: "png" | "jpeg",
    ): BookModel.Id | CommonError.CommonErrors;
  }
}
