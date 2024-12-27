use actix_web::{web, App, HttpResponse, HttpServer, get, post, patch};
use serde::{Deserialize, Serialize};
use sqlx::{postgres::PgPoolOptions, Pool, Postgres, Row};
use std::env;

#[derive(Debug, Serialize, Deserialize, sqlx::FromRow)]
struct MenuItem {
    #[serde(skip_deserializing)]
    id: Option<i32>,
    name: String,
    price: i32,
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
        Ok(items) => {
            println!("Successfully fetched {} menu items", items.len());
            HttpResponse::Ok().json(items)
        },
        Err(e) => {
            eprintln!("Database error: {}", e);
            HttpResponse::InternalServerError().json("Failed to fetch menu items")
        }
    }
}

#[post("/menu")]
async fn create_menu_item(item: web::Json<MenuItem>, data: web::Data<AppState>) -> HttpResponse {
    println!("Attempting to create menu item: {:?}", item);
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
            println!("Successfully created menu item with id: {}", id);
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
    println!("Attempting to update menu item {}: {:?}", id, item);
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
        Ok(_) => {
            println!("Successfully updated menu item {}", id);
            HttpResponse::Ok().finish()
        }
        Err(e) => {
            eprintln!("Database error: {}", e);
            HttpResponse::InternalServerError().json("Failed to update menu item")
        }
    }
}

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let database_url = env::var("DATABASE_URL")
        .unwrap_or_else(|_| "postgres://restaurant:devpassword@host.minikube.internal:5432/restaurant".to_string());

    println!("Starting menu service with following configuration:");
    println!("Database URL: {}", database_url);

    println!("Attempting to create database pool...");
    let pool = match PgPoolOptions::new()
        .max_connections(5)
        .connect(&database_url)
        .await {
            Ok(pool) => {
                println!("Successfully created database pool");
                pool
            },
            Err(e) => {
                eprintln!("Failed to create pool: {}", e);
                panic!("Failed to create pool: {}", e);
            }
        };

    // First, get the table schema
    println!("Checking existing table schema...");
    let table_exists = sqlx::query("SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'menu_items')")
        .fetch_one(&pool)
        .await
        .map(|row| row.get::<bool, _>(0))
        .unwrap_or(false);

    if table_exists {
        println!("Table exists, dropping it to recreate with correct schema");
        match sqlx::query("DROP TABLE menu_items")
            .execute(&pool)
            .await {
                Ok(_) => println!("Successfully dropped existing table"),
                Err(e) => {
                    eprintln!("Failed to drop table: {}", e);
                    panic!("Failed to drop table: {}", e);
                }
            };
    }

    println!("Creating menu_items table with INTEGER price...");
    match sqlx::query(
        "CREATE TABLE menu_items (
            id SERIAL PRIMARY KEY,
            name VARCHAR NOT NULL,
            price INTEGER NOT NULL,
            is_available BOOLEAN NOT NULL
        )",
    )
    .execute(&pool)
    .await {
        Ok(_) => println!("Table creation successful"),
        Err(e) => {
            eprintln!("Failed to create table: {}", e);
            panic!("Failed to create table: {}", e);
        }
    }

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