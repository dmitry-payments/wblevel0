-- +goose Up
-- +goose StatementBegin

CREATE table "orders"(
    order_uid varchar(255) primary key,
    track_number varchar(255),
    entry varchar(255),
    delivery_name varchar(255),
    delivery_phone varchar(255),
    delivery_zip varchar(255),
    delivery_city varchar(255),
    delivery_address varchar(255),
    delivery_region varchar(255),
    delivery_email varchar(255),
    payment_transaction varchar(255),
    payment_request_id varchar(255),
    payment_currency varchar(255),
    payment_provider varchar(255),
    payment_amount int,
    payment_payment_dt int,
    payment_bank varchar(255),
    payment_delivery_cost int,
    payment_goods_total int,
    payment_custom_fee int,
    locale varchar(255),
    internal_signature varchar(255),
    customer_id varchar(255),
    delivery_service varchar(255),
    shardkey varchar(255),
    sm_id int,
    date_created varchar(255),
    oof_shard varchar(255)
);
CREATE table "items"(
    id serial primary key,
    order_uid varchar(255),
    chrt_id int,
    track_number varchar(255),
    price int,
    rid varchar(255),
    name varchar(255),
    sale int,
    size varchar(255),
    total_price int,
    nm_id int,
    brand varchar(255),
    status int
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP table "orders";
DROP table "items";
-- +goose StatementEnd
