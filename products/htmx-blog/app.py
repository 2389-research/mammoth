# ABOUTME: Main Flask application for the HTMX blog platform.
# ABOUTME: Handles routing, database operations, and markdown rendering for a simple blog.

import sqlite3
import markdown
from datetime import datetime
from flask import (
    Flask,
    render_template,
    request,
    g,
    make_response,
)

app = Flask(__name__)
app.secret_key = "htmx-blog-secret-key-1337"

DATABASE = "blog.db"


def get_db():
    """Get a database connection for the current request."""
    if "db" not in g:
        g.db = sqlite3.connect(DATABASE)
        g.db.row_factory = sqlite3.Row
    return g.db


@app.teardown_appcontext
def close_db(exception):
    """Close the database connection at the end of each request."""
    db = g.pop("db", None)
    if db is not None:
        db.close()


def init_db():
    """Create the posts table if it doesn't exist and seed sample data."""
    db = sqlite3.connect(DATABASE)
    db.row_factory = sqlite3.Row
    db.execute(
        """
        CREATE TABLE IF NOT EXISTS posts (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            title TEXT NOT NULL,
            body TEXT NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    """
    )
    db.commit()

    # Seed sample posts if the table is empty
    row = db.execute("SELECT COUNT(*) as cnt FROM posts").fetchone()
    if row["cnt"] == 0:
        seed_posts = [
            {
                "title": "Welcome to the HTMX Blog",
                "body": (
                    "# Hello, World!\n\n"
                    "This is a blog built with **Flask** and **HTMX**. "
                    "No frontend framework, no build step, just clean HTML "
                    "enhanced with hypermedia.\n\n"
                    "## Features\n\n"
                    "- Create, edit, and delete posts\n"
                    "- Markdown rendering\n"
                    "- Dynamic updates without page reloads\n"
                    "- Clean, minimal design\n\n"
                    "Enjoy writing!"
                ),
            },
            {
                "title": "Why HTMX?",
                "body": (
                    "HTMX lets you build modern, dynamic web applications "
                    "without writing JavaScript.\n\n"
                    "Instead of shipping a massive JS bundle to the browser, "
                    "you return **HTML fragments** from the server and let HTMX "
                    "swap them into the page.\n\n"
                    "### The philosophy\n\n"
                    "> Hypermedia as the engine of application state.\n\n"
                    "It's REST the way it was meant to be. Your server returns "
                    "representations, your client renders them. Simple."
                ),
            },
            {
                "title": "Markdown Tips",
                "body": (
                    "This blog supports **Markdown** for formatting your posts. "
                    "Here are some tips:\n\n"
                    "### Text Formatting\n\n"
                    "- **Bold** with `**double asterisks**`\n"
                    "- *Italic* with `*single asterisks*`\n"
                    "- `Code` with backticks\n\n"
                    "### Code Blocks\n\n"
                    "```python\n"
                    "def hello():\n"
                    '    print("Hello from the blog!")\n'
                    "```\n\n"
                    "### Links\n\n"
                    "[Like this](https://example.com)\n\n"
                    "Happy writing!"
                ),
            },
        ]
        now = datetime.now().isoformat()
        for post in seed_posts:
            db.execute(
                "INSERT INTO posts (title, body, created_at, updated_at) VALUES (?, ?, ?, ?)",
                (post["title"], post["body"], now, now),
            )
        db.commit()

    db.close()


def render_markdown(text):
    """Convert markdown text to HTML."""
    return markdown.markdown(text, extensions=["fenced_code", "tables"])


def flash_message(message, category="success"):
    """Generate an OOB swap HTML snippet for flash messages."""
    return (
        f'<div id="flash-messages" hx-swap-oob="innerHTML">'
        f'<div class="flash flash-{category}">{message}</div>'
        f"</div>"
    )


def get_post_preview(body, max_length=200):
    """Return a plain text preview of a markdown post body."""
    # Strip markdown formatting for a clean preview
    plain = body.replace("#", "").replace("*", "").replace("`", "").replace(">", "")
    plain = " ".join(plain.split())
    if len(plain) > max_length:
        return plain[:max_length] + "..."
    return plain


@app.route("/")
def index():
    """Display the homepage with all blog posts."""
    db = get_db()
    posts = db.execute(
        "SELECT * FROM posts ORDER BY created_at DESC"
    ).fetchall()
    posts_with_preview = []
    for post in posts:
        posts_with_preview.append(
            {
                "id": post["id"],
                "title": post["title"],
                "body": post["body"],
                "preview": get_post_preview(post["body"]),
                "created_at": post["created_at"],
                "updated_at": post["updated_at"],
            }
        )
    return render_template("index.html", posts=posts_with_preview)


@app.route("/posts/new")
def new_post():
    """Display the create post form."""
    return render_template("post_form.html", post=None)


@app.route("/posts", methods=["POST"])
def create_post():
    """Create a new blog post and return the updated post list."""
    title = request.form.get("title", "").strip()
    body = request.form.get("body", "").strip()

    if not title or not body:
        response = make_response(
            flash_message("Title and body are required.", "error")
        )
        response.status_code = 422
        return response

    db = get_db()
    now = datetime.now().isoformat()
    db.execute(
        "INSERT INTO posts (title, body, created_at, updated_at) VALUES (?, ?, ?, ?)",
        (title, body, now, now),
    )
    db.commit()

    # Re-fetch all posts and return the full list with a flash message
    posts = db.execute(
        "SELECT * FROM posts ORDER BY created_at DESC"
    ).fetchall()
    posts_with_preview = []
    for post in posts:
        posts_with_preview.append(
            {
                "id": post["id"],
                "title": post["title"],
                "body": post["body"],
                "preview": get_post_preview(post["body"]),
                "created_at": post["created_at"],
                "updated_at": post["updated_at"],
            }
        )

    html = render_template("index.html", posts=posts_with_preview)
    html += flash_message("Post created successfully!")
    response = make_response(html)
    response.headers["HX-Push-Url"] = "/"
    return response


@app.route("/posts/<int:post_id>")
def view_post(post_id):
    """Display a single blog post with rendered markdown."""
    db = get_db()
    post = db.execute("SELECT * FROM posts WHERE id = ?", (post_id,)).fetchone()
    if post is None:
        response = make_response(flash_message("Post not found.", "error"))
        response.status_code = 404
        return response

    rendered_body = render_markdown(post["body"])
    return render_template("post.html", post=post, rendered_body=rendered_body)


@app.route("/posts/<int:post_id>/edit")
def edit_post(post_id):
    """Return the edit form partial for a post."""
    db = get_db()
    post = db.execute("SELECT * FROM posts WHERE id = ?", (post_id,)).fetchone()
    if post is None:
        response = make_response(flash_message("Post not found.", "error"))
        response.status_code = 404
        return response

    return render_template("post_form.html", post=post)


@app.route("/posts/<int:post_id>", methods=["PUT"])
def update_post(post_id):
    """Update an existing blog post."""
    title = request.form.get("title", "").strip()
    body = request.form.get("body", "").strip()

    if not title or not body:
        response = make_response(
            flash_message("Title and body are required.", "error")
        )
        response.status_code = 422
        return response

    db = get_db()
    now = datetime.now().isoformat()
    db.execute(
        "UPDATE posts SET title = ?, body = ?, updated_at = ? WHERE id = ?",
        (title, body, now, post_id),
    )
    db.commit()

    post = db.execute("SELECT * FROM posts WHERE id = ?", (post_id,)).fetchone()
    rendered_body = render_markdown(post["body"])

    html = render_template("post.html", post=post, rendered_body=rendered_body)
    html += flash_message("Post updated successfully!")
    return html


@app.route("/posts/<int:post_id>", methods=["DELETE"])
def delete_post(post_id):
    """Delete a blog post and return the updated post list."""
    db = get_db()
    db.execute("DELETE FROM posts WHERE id = ?", (post_id,))
    db.commit()

    # Return updated post list
    posts = db.execute(
        "SELECT * FROM posts ORDER BY created_at DESC"
    ).fetchall()
    posts_with_preview = []
    for post in posts:
        posts_with_preview.append(
            {
                "id": post["id"],
                "title": post["title"],
                "body": post["body"],
                "preview": get_post_preview(post["body"]),
                "created_at": post["created_at"],
                "updated_at": post["updated_at"],
            }
        )

    html = render_template("index.html", posts=posts_with_preview)
    html += flash_message("Post deleted.")
    response = make_response(html)
    response.headers["HX-Push-Url"] = "/"
    return response


@app.route("/posts/<int:post_id>/confirm-delete")
def confirm_delete(post_id):
    """Return the delete confirmation partial."""
    db = get_db()
    post = db.execute("SELECT * FROM posts WHERE id = ?", (post_id,)).fetchone()
    if post is None:
        response = make_response(flash_message("Post not found.", "error"))
        response.status_code = 404
        return response

    return render_template("confirm_delete.html", post=post)


if __name__ == "__main__":
    init_db()
    app.run(debug=True, port=1337)
