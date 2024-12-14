// menu/src/main.rs
use actix_web::{web, App, HttpResponse, HttpServer, get, post, patch};
use serde::{Deserialize, Serialize};
use sqlx::{postgres::PgPoolOptions, Pool, Postgres};
use std::env;

#[derive(Debug, Serialize, Deserialize, sqlx::FromRow)]
struct MenuItem {
    #[serde(skip_deserializing)]
    id: Option<i32>,
    name: String,
    price: f64,
    is_available: bool,
}

struct AppState {
    db: Pool<Postgres>,
}

#[get("/menu")]
async fn get_menu_items(data: web::Data<AppState>) -> HttpResponse {
    match sqlx::query_as::<_, MenuItem>("SELECT * FROM menu_items")
        .fetch_all(&data.db)
        .await
    {
        Ok(items) => HttpResponse::Ok().json(items),
        Err(e) => {
            eprintln!("Database error: {}", e);
            HttpResponse::InternalServerError().json("Failed to fetch menu items")
        }
    }
}

#[post("/menu")]
async fn create_menu_item(item: web::Json<MenuItem>, data: web::Data<AppState>) -> HttpResponse {
    match sqlx::query(
        "INSERT INTO menu_items (name, price, is_available) VALUES ($1, $2, $3) RETURNING id",
    )
    .bind(&item.name)
    .bind(item.price)
    .bind(item.is_available)
    .fetch_one(&data.db)
    .await
    {
        Ok(row) => {
            let id: i32 = row.get(0);
            HttpResponse::Created().json(id)
        }
        Err(e) => {
            eprintln!("Database error: {}", e);
            HttpResponse::InternalServerError().json("Failed to create menu item")
        }
    }
}

#[patch("/menu/{id}")]
async fn update_menu_item(
    path: web::Path<i32>,
    item: web::Json<MenuItem>,
    data: web::Data<AppState>,
) -> HttpResponse {
    let id = path.into_inner();
    match sqlx::query(
        "UPDATE menu_items SET name = $1, price = $2, is_available = $3 WHERE id = $4",
    )
    .bind(&item.name)
    .bind(item.price)
    .bind(item.is_available)
    .bind(id)
    .execute(&data.db)
    .await
    {
        Ok(_) => HttpResponse::Ok().finish(),
        Err(e) => {
            eprintln!("Database error: {}", e);
            HttpResponse::InternalServerError().json("Failed to update menu item")
        }
    }
}

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let database_url = env::var("DATABASE_URL")
        .unwrap_or_else(|_| "postgres://restaurant:devpassword@10.0.2.15:5432/restaurant".to_string());

    let pool = PgPoolOptions::new()
        .max_connections(5)
        .connect(&database_url)
        .await
        .expect("Failed to create pool");

    // Create table if not exists
    sqlx::query(
        "CREATE TABLE IF NOT EXISTS menu_items (
            id SERIAL PRIMARY KEY,
            name VARCHAR NOT NULL,
            price DECIMAL NOT NULL,
            is_available BOOLEAN NOT NULL
        )",
    )
    .execute(&pool)
    .await
    .expect("Failed to create table");

    let state = web::Data::new(AppState { db: pool });

    println!("Starting server at http://0.0.0.0:8080");

    HttpServer::new(move || {
        App::new()
            .app_data(state.clone())
            .service(get_menu_items)
            .service(create_menu_item)
            .service(update_menu_item)
    })
    .bind("0.0.0.0:8080")?
    .run()
    .await
}