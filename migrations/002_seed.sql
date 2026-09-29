-- 只补充缺失的演示商品；重复执行不会重置已经卖出的库存。
INSERT INTO products (id, name, price_cents, initial_stock, stock, starts_at, ends_at)
VALUES (1, 'Go 工程师机械键盘', 9900, 100, 100, UTC_TIMESTAMP(6) - INTERVAL 1 HOUR, UTC_TIMESTAMP(6) + INTERVAL 1 DAY)
ON DUPLICATE KEY UPDATE id = products.id;
INSERT INTO products (id, name, price_cents, initial_stock, stock, starts_at, ends_at)
VALUES (2, '程序员桌面台灯', 3900, 25, 25, UTC_TIMESTAMP(6) - INTERVAL 1 HOUR, UTC_TIMESTAMP(6) + INTERVAL 1 DAY)
ON DUPLICATE KEY UPDATE id = products.id;
