package confload_test

import (
	"fmt"
	"os"

	"github.com/velmie/x/svc/confload"
)

type exampleInspectionConfig struct {
	Name string `k:"name" default:"world"`
}

func ExampleInspect() {
	fields, err := confload.DescribeStruct[exampleInspectionConfig]()
	if err != nil {
		fmt.Println(err)
		return
	}
	loader := confload.New(confload.WithEnvPrefix("APP_"))
	rows, err := confload.Inspect(
		fields,
		exampleInspectionConfig{
			Name: "reader",
		},
		confload.Snapshot{},
		confload.InspectOptions{
			EnvName: loader.EnvName,
		},
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(rows[0].Field.Path, rows[0].Env, rows[0].Value)
	// Output: name APP_NAME reader
}

func ExampleInspect_describe() {
	fields, err := confload.DescribeStruct[exampleInspectionConfig]()
	if err != nil {
		fmt.Println(err)
		return
	}
	rows, err := confload.Inspect(fields, nil, confload.Snapshot{}, confload.InspectOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(rows[0].Field.Path, rows[0].Field.Default, rows[0].Origin == nil)
	// Output: name world true
}

func ExampleWriteInspection() {
	fields, err := confload.DescribeStruct[exampleInspectionConfig]()
	if err != nil {
		fmt.Println(err)
		return
	}
	rows, err := confload.Inspect(fields, nil, confload.Snapshot{}, confload.InspectOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := confload.WriteInspection(os.Stdout, "text", rows); err != nil {
		fmt.Println(err)
	}
	// Output: name type=string env=NAME default="world" required=false sensitive=false validation="" hint=""
}

func ExampleParseOverrides() {
	fields, err := confload.DescribeStruct[exampleInspectionConfig]()
	if err != nil {
		fmt.Println(err)
		return
	}
	values, err := confload.ParseOverrides(fields, []string{"name=first", "name=hello=world"})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(values["name"])
	// Output: hello=world
}
