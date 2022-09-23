# Temporal gRPC API

Proto files describing our gRPC API. Implemented by [Temporal Server](https://github.com/temporalio/temporal) and used by the [SDKs](https://docs.temporal.io/temporal#temporal-sdk) (both Clients and Workers).

- [Use the API](#use-the-api)
  - [With code](#with-code)
    - [Generate client stubs](#generate-client-stubs)
  - [Via REST](#via-rest)
  - [Via GraphQL](#via-graphql)
  - [Manually](#manually)
    - [With command line](#with-command-line)
    - [With a GUI](#with-a-gui)
- [License](#license)

## Use the API

### With code

Usually you interact with the API via high-level SDK methods like `startWorkflow()`. However, Clients also expose the underlying gRPC services, like:

- [`Client.connection.workflowService`](https://typescript.temporal.io/api/classes/client.connection/#workflowservice)
- [`Client.connection.healthService`](https://typescript.temporal.io/api/classes/client.connection/#healthservice)
- [`Client.connection.operatorService`](https://typescript.temporal.io/api/classes/client.connection/#operatorservice)

#### Generate client stubs

If you're not using an SDK Client (rare), you can generate gRPC client stubs by:

- Adding this repo as a git submodule or subtree inside your repo
- Generating code in [your language](https://grpc.io/docs/languages/)

### Via REST

See [`temporalio/ui-server`](https://github.com/temporalio/ui-server)

### Via GraphQL

See [`temporalio/graphql`](https://github.com/temporalio/graphql)

### Manually

To query the API manually via command line or a GUI, first:

- Run Temporal Server locally ([Temporalite or Docker Compose](https://docs.temporal.io/application-development/foundations#run-a-dev-cluster))
- Clone this repo:

  ```sh
  git clone https://github.com/temporalio/api.git
  cd api
  ```

#### With command line

Install [`evans`](https://github.com/ktr0731/evans#installation).

```
cd /path/to/api
evans --proto temporal/api/workflowservice/v1/service.proto --port 7233
```

#### With a GUI

- Install [BloomRPC](https://github.com/bloomrpc/bloomrpc#installation).
- Open the app
- Select "Import Paths" button on the top-left and enter the path to the cloned repo: `/path/to/api`
- Select the "Import protos" + button and select this file:

  `/path/to/api/temporal/api/workflowservice/v1/service.proto`

- A list of methods should appear in the sidebar. Select one.
- Edit the JSON in the left pane.
- Hit `Cmd-Enter` or click the play button to get a response from the server on the right.

![ListWorkflowExecutions](https://www.dropbox.com/s/ahuqk09ypoy79vq/BloomRPC.png?raw=1)

One downside compared to [command line](#with-command-line) is it doesn't show enum names, just numbers like `"task_queue_type": 1`.

![DescribeTaskQueue](https://www.dropbox.com/s/2pi21trui7l678p/DescribeTaskQueue.png?raw=1)

## License

[MIT License](LICENSE)
