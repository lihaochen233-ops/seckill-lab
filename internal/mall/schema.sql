CREATE TABLE IF NOT EXISTS mall_users (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 email VARCHAR(160) NOT NULL UNIQUE,
 name VARCHAR(80) NOT NULL,
 password_hash VARCHAR(256) NOT NULL,
 role VARCHAR(16) NOT NULL DEFAULT 'customer',
 created_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS mall_products (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 name VARCHAR(120) NOT NULL,
 subtitle VARCHAR(240) NOT NULL,
 description TEXT NOT NULL,
 category VARCHAR(40) NOT NULL,
 image VARCHAR(80) NOT NULL,
 price_cents BIGINT NOT NULL,
 original_price_cents BIGINT NOT NULL,
 stock BIGINT NOT NULL,
 initial_stock BIGINT NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'active',
 featured INTEGER NOT NULL DEFAULT 0,
 created_at BIGINT NOT NULL,
 CHECK(stock>=0 AND price_cents>0)
);
CREATE TABLE IF NOT EXISTS mall_addresses (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 user_id BIGINT NOT NULL,
 recipient VARCHAR(120) NOT NULL,
 phone VARCHAR(30) NOT NULL,
 region VARCHAR(240) NOT NULL,
 detail VARCHAR(600) NOT NULL,
 FOREIGN KEY(user_id) REFERENCES mall_users(id)
);
CREATE TABLE IF NOT EXISTS mall_cart (
 user_id BIGINT NOT NULL,
 product_id BIGINT NOT NULL,
 quantity BIGINT NOT NULL,
 PRIMARY KEY(user_id,product_id),
 FOREIGN KEY(user_id) REFERENCES mall_users(id),
 FOREIGN KEY(product_id) REFERENCES mall_products(id),
 CHECK(quantity>0 AND quantity<=99)
);
CREATE TABLE IF NOT EXISTS mall_activities (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 product_id BIGINT NOT NULL,
 price_cents BIGINT NOT NULL,
 initial_stock BIGINT NOT NULL,
 stock BIGINT NOT NULL,
 returned BIGINT NOT NULL DEFAULT 0,
 starts_at BIGINT NOT NULL,
 ends_at BIGINT NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'paused',
 epoch VARCHAR(64) NOT NULL,
 FOREIGN KEY(product_id) REFERENCES mall_products(id),
 CHECK(stock>=0 AND returned>=0 AND stock+returned<=initial_stock AND ends_at>starts_at)
);
CREATE TABLE IF NOT EXISTS mall_tickets (
 id VARCHAR(48) PRIMARY KEY,
 user_id BIGINT NOT NULL,
 activity_id BIGINT NOT NULL,
 epoch VARCHAR(64) NOT NULL,
 address_json TEXT NOT NULL,
 request_key VARCHAR(64) NOT NULL,
 status VARCHAR(16) NOT NULL,
 reason VARCHAR(240) NOT NULL DEFAULT '',
 order_id VARCHAR(48) NOT NULL DEFAULT '',
 created_at BIGINT NOT NULL,
 UNIQUE(user_id,activity_id),
 FOREIGN KEY(user_id) REFERENCES mall_users(id),
 FOREIGN KEY(activity_id) REFERENCES mall_activities(id)
);
CREATE TABLE IF NOT EXISTS mall_orders (
 id VARCHAR(48) PRIMARY KEY,
 user_id BIGINT NOT NULL,
 request_key VARCHAR(80) NOT NULL,
 fingerprint VARCHAR(64) NOT NULL,
 kind VARCHAR(16) NOT NULL,
 status VARCHAR(16) NOT NULL,
 total_cents BIGINT NOT NULL,
 items_json TEXT NOT NULL,
 address_json TEXT NOT NULL,
 activity_id BIGINT NOT NULL DEFAULT 0,
 ticket_id VARCHAR(48) NOT NULL DEFAULT '',
 created_at BIGINT NOT NULL,
 expires_at BIGINT NOT NULL,
 paid_at BIGINT NOT NULL DEFAULT 0,
 tracking VARCHAR(100) NOT NULL DEFAULT '',
 UNIQUE(user_id,request_key),
 FOREIGN KEY(user_id) REFERENCES mall_users(id),
 CHECK(total_cents>0)
);
CREATE TABLE IF NOT EXISTS mall_payments (
 order_id VARCHAR(48) PRIMARY KEY,
 amount_cents BIGINT NOT NULL,
 provider VARCHAR(20) NOT NULL,
 status VARCHAR(16) NOT NULL,
 created_at BIGINT NOT NULL,
 FOREIGN KEY(order_id) REFERENCES mall_orders(id)
);
CREATE TABLE IF NOT EXISTS mall_outbox (
 id VARCHAR(100) PRIMARY KEY,
 kind VARCHAR(32) NOT NULL,
 payload TEXT NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'pending',
 attempts INTEGER NOT NULL DEFAULT 0,
 available_at BIGINT NOT NULL,
 lease_until BIGINT NOT NULL DEFAULT 0,
 last_error VARCHAR(240) NOT NULL DEFAULT '',
 created_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS mall_audit (
 id BIGINT PRIMARY KEY AUTO_INCREMENT,
 actor_id BIGINT NOT NULL,
 action VARCHAR(60) NOT NULL,
 target VARCHAR(100) NOT NULL,
 created_at BIGINT NOT NULL
);
