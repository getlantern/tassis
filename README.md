The most up-to-date documentation lives in the code. See [model/messages.go](model/messages.go) for message formats and [testsupport/testsupport.go](testsupport/testsupport.go) for a fairly full usage example
that illustrates the message exchange pattern.

### Protocol Buffers
This project uses protocol buffers. Follow the [tutorial](https://developers.google.com/protocol-buffers/docs/gotutorial) to ensure that you have the right tools in place, then run `make` to ensure the protocol buffers are up to date.

# messaging-server

messaging-server provides back-end facilities for exchanging end-to-end encryptoed (E2EE) messages between messaging clients. It provides two key facilities:

- Key Distribution
- Message transport

## Key Distribution
Encryption is handled by clients using [Signal](https://github.com/signalapp/libsignal-protocol-java). messaging-server implements the "server" role as defined in Signal's [Sesame paper](https://www.signal.org/docs/specifications/sesame/).

Clients identify each other by a combination of userID and deviceID.

### UserID (also Identity Key)
A 33 bit unique identifier for a user to which messages can be delivered, and also that user's public key.

### DeviceID
A 32 bit unsigned integer that uniquely identifies one of a user's devices (not unique across users).

With Signal, messages are actually exchanged between devices, with each device pair having its own crypto session. So for example, if user A wants to send a message to user B
that can be read on all their devices, user A needs to establish independent sessions with all of user B's devices, independently encrypt the message and send the separate ciphertext to each device.

Each device is identified by an address.

### Address
The unique combination of UserID and DeviceID constitutes an address.

### Sealed Sender
A scheme for allowing senders to send messages without intermediaries know who sent the message.

Signal clients use the [X3DH key agreement protocol](https://www.signal.org/docs/specifications/x3dh/) to establish encrypted session, which requires server-facilitiated exchange of key information.

messaging-server is ignorant of Signal's encryption algorithms and simply stores the key information for use by Signal's client-side encryption logic as opaque data.

There are two ways that a session can be established:

### Sender Session Creation
When a device wants to send an encrypted message to a user, it initiates one local session per recipient device. In order to do this, it needs to know the following for each device:

- Registration ID
- IdentityKey
- SignedPreKey
- PreKey

#### Registration ID
This is a random uint32 associated with the device. *It's not entirely clear to me how Signal uses this*

#### SignedPreKey
This is a long lived preKey used by X3DH. Typically there's only one of these per device, though it can be changed over time. See [here](https://crypto.stackexchange.com/questions/72148/signal-protocol-how-is-signed-preKey-created)
for some more explanation.

#### PreKey
This is a disposable, one-time preKey used for session establishment. Clients generate these in batches and send them to messaging-server for storage. When other clients request preKeys for specific devices, the server pops one off the list of available preKeys so that it cannot be used again. If there are no preKeys available, the server simply returns an empty value-X3DH can still proceeed without this.

messaging-server provides a facility to allow clients to obtain this information for any userID and deviceID registered on the network.

### Recipient Session Creation
Recipients of encrypted messages initiate their end of the session using information that the sender included in their initial message. This has no dependency on any facilities provided by messaging-server.

## Message transport
messaging-server facilities the exchange of messages between clients by supporting store-and-forward send and receive of opaque messages between clients. After connecting to messaging-server, clients can send messages to arbitrary addresses. Connected clients identify their own address upon connecting, at which point they can receive any messages that were sent to them.

### Message Acknowledgement
Once clients have durably received a message, they acknowledge this to messaging-server, which in turn acknowledges receipt of the message to its broker so that the message can be deleted and won't be delivered in the future.

### Message Retention
We still need to work this out, but for practical reasons, messages won't be retained forever.

## Websockets API
The public API uses websockets. Clients connect to ws[s]://server/<userID>/<deviceID>. At that point, they exchange messages with the server, both for accessing key distribution functions as well as sending messages to other users.

All messages in the context of a client connection have a unique sequence number that identifies the message (separate sequences for both directions).

For all messages, if an error is encountered while processing the message, the server will respond with an [error message](model/errors.go) whose sequence number is set to the sequence number of the message that led to the error.

For messages that require a response (like RequestPreKeys), if there was no error, the remote end will respond the corresponding response messages (like PreKey).

For all other messages, the remote end (both client and server) should respond with an ACK message whose sequence number is set to the sequence number of the message that is being acknowledged.

### Message Types
The implementation of the messages types lives at [model/messages.go](model/messages.go).

### Message Version
The message envelope makes a provision for supporting different versions of the message formats, though currently we have only 1 version.

## External Dependencies
messaging-server needs a [database](db/db.go) for storing key distribution information and a pub/sub [message broker](broker.broker.go) for exchanging messages between users.

Simple in-memory implementations of both are provided for testing. The database and broker to be used for production still need to be selected, though [DynamoDB](https://aws.amazon.com/dynamodb/) and [Apache Pulsar](https://streamnative.io/cloud/hosted) are current front-runners.

## Security

### Authentication
messaging-server supports both authenticated and unauthenticated connections. At the beginning of every connection, the server sends an authentication challenge to the client with a nonce. Clients that wish to remain anonymous can simply ignore the challenge. Clients that wish to authenticate response with theri Address (UserID and DeviceID), the nonce from the challenge, and a signature over the Address+Nonce. The server then verifies that the nonce matches the expected value for this connection and that the signature is correct correct based on the sender's public key (which is also their userID). If yes, the user is authenticated. If not, the server returns an error and closes the connection.

Clients request pre-keys and send messages on unauthenticated connections in order to protect their anonymity. All other key management operations are performed on an authenticated connection.

### Key Distribution
In principle, messaging-server only provides a convenience for key-distribution, and it's encumbent on clients and end-users to verify key material for themselves, not least because they shouldn't blindly trust messaging-server itself.

In practice, since end-users of clients that connect with messaging-server are also registered Lantern users, messaging-server can ensure that registration of key material to a specific userID is only performed by someone whose Lantern account is associated with that userID.

### Transport Security

#### Message Privacy and Integrity
Because clients use E2EE, messaging-server does not concern itself with protecting the contents of messages from eavesdropping or tampering

#### Denial of Service
Because clients rely on messaging-server for the actual transport of messages, it is important to guard against various denial of services attacks. In addition to the typical denial of service attacks faced by any web service, messaging-server guards against the following categories of attack:

##### Message Flooding
Rate limiting should be used to prevent individual clients from flooding the network, or any particular user, with messages. *this is not yet implemented*

##### Message Stealing
In order to prevent unauthorized users from stealing messages before they can be received by their legitimate clients, messaging-server authenticates clients based on Lantern ID to make sure that only authorized clients may read messages on behalf of a specific user.

### Metadata
messaging-server does not provide any assurances about protecting knowledge about the relationships between senders and recipients (i.e. who has sent to whom and when).

