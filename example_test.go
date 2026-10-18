package edgar_test

import (
	"context"
	"fmt"
	"log"

	"github.com/andrewgould27/go-edgar"
)

func ExampleClient_SubmissionsByTicker() {
	c := edgar.NewClient("MyApp you@example.com")

	s, err := c.SubmissionsByTicker(context.Background(), "AAPL")
	if err != nil {
		log.Fatal(err)
	}

	if tenKs := s.FilingsByForm("10-K"); len(tenKs) > 0 {
		fmt.Println(s.Name, "latest 10-K:", tenKs[0].URL())
	}
}
