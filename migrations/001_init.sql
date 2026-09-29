CREATE TABLE IF NOT EXISTS products (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    name VARCHAR(120) NOT NULL,
    price_cents BIGINT NOT NULL CHECK (price_cents >= 0),
    initial_stock BIGINT NOT NULL CHECK (initial_stock >= 0),
    stock BIGINT NOT NULL,
    starts_at DATETIME(6) NOT NULL,
    ends_at DATETIME(6) NOT NULL,
    CHECK (ends_at > starts_at),
    CHECK (stock >= 0 AND stock <= initial_stock)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS orders (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL CHECK (user_id > 0),
    product_id BIGINT NOT NULL,
    request_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    price_cents BIGINT NOT NULL CHECK (price_cents >= 0),
    created_at DATETIME(6) NOT NULL,
    CONSTRAINT uq_order_request UNIQUE (user_id, request_key),
    CONSTRAINT uq_order_purchase UNIQUE (user_id, product_id),
    CONSTRAINT fk_order_product FOREIGN KEY (product_id) REFERENCES products(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
