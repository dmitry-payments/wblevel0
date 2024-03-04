package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/nats-io/stan.go"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}

func setupRoutes(router *gin.Engine, cache *map[string]payload) {

	router.GET("/order/:id", func(c *gin.Context) {
		orderID := c.Param("id")
		order, ok := (*cache)[orderID]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
			return
		}
		c.JSON(http.StatusOK, order)
	})

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}

func run() error {

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	log.Println("Connect to NATS Streaming")
	sc, err := stan.Connect("test-cluster", "wbLevel0", stan.NatsURL("nats://localhost:4223"))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer sc.Close()

	config, err := pgxpool.ParseConfig("host=localhost user=level0 password=example dbname=level0 sslmode=disable port=5433")
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	log.Println("Connect to Postgres")
	conn, err := pgxpool.ConnectConfig(ctx, config)
	if err != nil {
		log.Fatalf("Unable to connect to the database: %v", err)
	}
	defer conn.Close()

	log.Println("Consuming...")

	cache, err := initializeCache(ctx, conn)
	if err != nil {
		return fmt.Errorf("initialize cache: %w", err)
	}

	router := gin.Default()

	setupRoutes(router, &cache)

	go func() {
		if err := router.Run(":3000"); err != nil {
			fmt.Printf("Error starting Gin server: %v\n", err)
		}
	}()

	sub, err := sc.Subscribe("orders1", func(m *stan.Msg) {
		fmt.Printf("Received a message: %s\n", string(m.Data))

		var receivedPayload payload

		err := json.Unmarshal(m.Data, &receivedPayload)
		if err != nil {
			log.Printf("Error decoding JSON: %s", err)
			return
		}

		log.Println("Saving data to the database")

		_, err = conn.Exec(ctx, `
			INSERT INTO orders (order_uid, track_number, entry, delivery_name, delivery_phone, 
			                    delivery_zip, delivery_city, delivery_address, 
			                    delivery_region, delivery_email, payment_transaction,
			                    payment_request_id, payment_currency, payment_provider, 
			                    payment_amount, payment_payment_dt, payment_bank, 
			                    payment_delivery_cost, payment_goods_total, payment_custom_fee, 
			                    locale, internal_signature, customer_id, delivery_service,
			                    shardkey, sm_id, date_created, oof_shard)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17,
			        $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28)`,
			receivedPayload.OrderUid, receivedPayload.TrackNumber, receivedPayload.Entry,
			receivedPayload.Delivery.Name, receivedPayload.Delivery.Phone,
			receivedPayload.Delivery.Zip, receivedPayload.Delivery.City,
			receivedPayload.Delivery.Address, receivedPayload.Delivery.Region, receivedPayload.Delivery.Email,
			receivedPayload.Payment.Transaction, receivedPayload.Payment.RequestId,
			receivedPayload.Payment.Currency, receivedPayload.Payment.Provider,
			receivedPayload.Payment.Amount, receivedPayload.Payment.PaymentDt,
			receivedPayload.Payment.Bank, receivedPayload.Payment.DeliveryCost,
			receivedPayload.Payment.GoodsTotal, receivedPayload.Payment.CustomFee,
			receivedPayload.Locale, receivedPayload.InternalSignature, receivedPayload.CustomerId,
			receivedPayload.DeliveryService, receivedPayload.ShardKey, receivedPayload.SmId,
			receivedPayload.DateCreated, receivedPayload.OofShard,
		)
		if err != nil {
			fmt.Println("Error inserting data into the database:", err)
			return
		}

		for _, item := range receivedPayload.Items {
			_, err = conn.Exec(ctx, `
			INSERT INTO items (order_uid, chrt_id, track_number, price, rid, name,
			                    sale, size, total_price, nm_id, brand, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
				receivedPayload.OrderUid, item.ChrtId, item.TrackNumber,
				item.Price, item.Rid, item.Name, item.Sale, item.Size, item.TotalPrice, item.NmId,
				item.Brand, item.Status,
			)
			if err != nil {
				fmt.Println("Error inserting data into the database:", err)
				return
			}
		}
		cache[receivedPayload.OrderUid] = receivedPayload
	})
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	_ = http.Server{
		Addr: ":3000",
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodGet {
				writer.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			path := request.URL.Path
			parts := strings.Split(path, "/")
			if len(parts) > 1 {
				value := parts[1]
				order, ok := cache[value]
				if !ok {
					writer.WriteHeader(http.StatusNotFound)
					return
				}
				body, err := json.Marshal(order)
				if err != nil {
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}
				writer.WriteHeader(http.StatusOK)
				writer.Header().Set("Content-Type", "application/json")
				writer.Write(body)
			} else {
				writer.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(writer, "Invalid path format")
			}

		}),
	}

	for {
		select {
		case <-ctx.Done():
			if err := sub.Unsubscribe(); err != nil {
				fmt.Printf("Error unsubscribing: %v\n", err)
			}
			fmt.Println("Subscriber closed.")
			return nil
		}
	}
	return nil
}

func initializeCache(ctx context.Context, conn *pgxpool.Pool) (map[string]payload, error) {
	cache := map[string]payload{}

	rows, err := conn.Query(ctx, `
SELECT order_uid, track_number, entry, delivery_name, delivery_phone,
		delivery_zip, delivery_city, delivery_address,
		delivery_region, delivery_email, payment_transaction,
		payment_request_id, payment_currency, payment_provider,
		payment_amount, payment_payment_dt, payment_bank,
		payment_delivery_cost, payment_goods_total, payment_custom_fee,
		locale, internal_signature, customer_id, delivery_service,
		shardkey, sm_id, date_created, oof_shard 
FROM orders`)
	if err != nil {
		return nil, fmt.Errorf("failed to query database: %w", err)

	}
	defer rows.Close()

	for rows.Next() {
		log.Println("Process order")
		var value payload
		if err := rows.Scan(&value.OrderUid, &value.TrackNumber, &value.Entry, &value.Delivery.Name, &value.Delivery.Phone,
			&value.Delivery.Zip, &value.Delivery.City, &value.Delivery.Address, &value.Delivery.Region, &value.Delivery.Email,
			&value.Payment.Transaction, &value.Payment.RequestId, &value.Payment.Currency, &value.Payment.Provider, &value.Payment.Amount,
			&value.Payment.PaymentDt, &value.Payment.Bank, &value.Payment.DeliveryCost, &value.Payment.GoodsTotal, &value.Payment.CustomFee,
			&value.Locale, &value.InternalSignature, &value.CustomerId, &value.DeliveryService, &value.ShardKey, &value.SmId,
			&value.DateCreated, &value.OofShard); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		items, err := scanItems(ctx, conn, value.OrderUid)
		if err != nil {
			return nil, fmt.Errorf("failed scan items: %w", err)
		}
		value.Items = items
		cache[value.OrderUid] = value
	}

	return cache, nil
}

func scanItems(ctx context.Context, conn *pgxpool.Pool, orderUid string) ([]payloadItem, error) {

	rows, err := conn.Query(ctx, `
SELECT  chrt_id, track_number, price, rid,
		name, sale, size, total_price, nm_id, brand,
		status
FROM items
WHERE order_uid = $1`, orderUid)
	if err != nil {
		return nil, fmt.Errorf("failed to query database: %w", err)

	}
	defer rows.Close()
	var items []payloadItem
	for rows.Next() {
		var value payloadItem
		if err := rows.Scan(&value.ChrtId, &value.TrackNumber, &value.Price, &value.Rid,
			&value.Name, &value.Sale, &value.Size, &value.TotalPrice, &value.NmId, &value.Brand,
			&value.Status); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		items = append(items, value)
	}

	return items, nil
}

type payload struct {
	OrderUid          string          `json:"order_uid"`
	TrackNumber       string          `json:"track_number"`
	Entry             string          `json:"entry"`
	Delivery          payloadDelivery `json:"delivery"`
	Payment           payloadPayment  `json:"payment"`
	Items             []payloadItem   `json:"items"`
	Locale            string          `json:"locale"`
	InternalSignature string          `json:"internal_signature"`
	CustomerId        string          `json:"customer_id"`
	DeliveryService   string          `json:"delivery_service"`
	ShardKey          string          `json:"shardkey"`
	SmId              int             `json:"sm_id"`
	DateCreated       string          `json:"date_created"`
	OofShard          string          `json:"oof_shard"`
}

type payloadDelivery struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Zip     string `json:"zip"`
	City    string `json:"city"`
	Address string `json:"address"`
	Region  string `json:"region"`
	Email   string `json:"email"`
}

type payloadPayment struct {
	Transaction  string `json:"transaction"`
	RequestId    string `json:"request_id"`
	Currency     string `json:"currency"`
	Provider     string `json:"provider"`
	Amount       int    `json:"amount"`
	PaymentDt    int    `json:"payment_dt"`
	Bank         string `json:"bank"`
	DeliveryCost int    `json:"delivery_cost"`
	GoodsTotal   int    `json:"goods_total"`
	CustomFee    int    `json:"custom_fee"`
}

type payloadItem struct {
	ChrtId      int    `json:"chrt_id"`
	TrackNumber string `json:"track_number"`
	Price       int    `json:"price"`
	Rid         string `json:"rid"`
	Name        string `json:"name"`
	Sale        int    `json:"sale"`
	Size        string `json:"size"`
	TotalPrice  int    `json:"total_price"`
	NmId        int    `json:"nm_id"`
	Brand       string `json:"brand"`
	Status      int    `json:"status"`
}
