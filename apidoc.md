# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [model/Messages.proto](#model/Messages.proto)
    - [Ack](#signal.Ack)
    - [Address](#signal.Address)
    - [AuthChallenge](#signal.AuthChallenge)
    - [AuthResponse](#signal.AuthResponse)
    - [Error](#signal.Error)
    - [Login](#signal.Login)
    - [Message](#signal.Message)
    - [OutboundMessage](#signal.OutboundMessage)
    - [PreKey](#signal.PreKey)
    - [PreKeysLow](#signal.PreKeysLow)
    - [Register](#signal.Register)
    - [RequestPreKeys](#signal.RequestPreKeys)
    - [Unregister](#signal.Unregister)
  
- [Scalar Value Types](#scalar-value-types)



<a name="model/Messages.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## model/Messages.proto
messaging-server uses an asynchronous messaging pattern for interacting with the API.

Clients typically connect to the messaging-server via WebSockets to exchange messages.

Clients will typically open two separate connections, authenticating on one and leaving
the other unauthenticated.

The unauthenticated connection is used for retrieving other users&#39; preKeys and
sending messages to them, so as not to reveal the identity of senders.

The authenticated connection is used for all other operations, including performing key
management and receiving messages from other users.

Authentication is performed using a challenge-response pattern in which the server sends
an authentication challenge to the client and the client responds with a signed authentication
response identifying its UserID and DeviceID. On anonymous connections, clients simply ignore the
authentication challenge.

Messages sent from clients to servers follow a request/response pattern. The server will always
respond to these with either an Ack or a typed response. In the event of an error, it will respond
with an Error message. This includes the following messages:

 - Register        -&gt; Ack
 - Unregister      -&gt; Ack
 - RequestPreKeys  -&gt; PreKey (may send multiple if there are multiple matching devices)
 - OutboundMessage -&gt; Ack

Some messages sent from the server to the client require an Ack in response:

 - inboundMessage  -&gt; Ack

Some messages don&#39;t require any response:

 - PreKeysLow

All messages sent within a given connection are identified by a unique sequence number (separate sequences
for each direction). When a response message is sent in either direction, its sequence number is set to the
message that triggered the response so that the client or server can correlate responses with requests.


<a name="signal.Ack"></a>

### Ack
Acknowledges successful receipt of a Message






<a name="signal.Address"></a>

### Address
An Address for a specific client


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| userID | [bytes](#bytes) |  | The 33 byte user ID, which is also the user&#39;s public key. It consists of a 1 byte type (always 0x05) followed by 32 bits of ed25519 public key |
| deviceID | [uint32](#uint32) |  | Identifier for a specific user device, only unique for a given userID |






<a name="signal.AuthChallenge"></a>

### AuthChallenge
A challenge to the client to authenticate. This is sent by the server once and only once, immediately after clients connect.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| nonce | [bytes](#bytes) |  | A nonce to identify this authentication exchange |






<a name="signal.AuthResponse"></a>

### AuthResponse
A response to an AuthChallenge that is sent from the client to the server on any connection that the client wishes to authenticate.
The server will accept an AuthResponse only once per connection.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| login | [bytes](#bytes) |  | The serialized form of the Login message |
| signature | [bytes](#bytes) |  | A signature of the serialized Login message calculated using the private key corresponding to the UserID that&#39;s logging in |






<a name="signal.Error"></a>

### Error
Indicates than an error occurred processing a request.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | An identifier for the error, like &#34;unknown_user&#34; |
| description | [string](#string) |  | Optional additional information about the error |






<a name="signal.Login"></a>

### Login
Login information supplied by clients in response to an AuthChallenge.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| address | [Address](#signal.Address) |  | The Address that&#39;s logging in. This will become permanently associated with the current connection |
| nonce | [bytes](#bytes) |  | This echoes back the nonce provided by the server in the AuthChallenge |






<a name="signal.Message"></a>

### Message
The envelope for all messages sent to/from clients.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sequence | [uint32](#uint32) |  | the message sequence, either a unique number for request messages or the number of the request message to which a response message corresponds |
| ack | [Ack](#signal.Ack) |  |  |
| error | [Error](#signal.Error) |  |  |
| authChallenge | [AuthChallenge](#signal.AuthChallenge) |  |  |
| authResponse | [AuthResponse](#signal.AuthResponse) |  |  |
| register | [Register](#signal.Register) |  |  |
| unregister | [Unregister](#signal.Unregister) |  |  |
| requestPreKeys | [RequestPreKeys](#signal.RequestPreKeys) |  |  |
| preKey | [PreKey](#signal.PreKey) |  |  |
| preKeysLow | [PreKeysLow](#signal.PreKeysLow) |  |  |
| outboundMessage | [OutboundMessage](#signal.OutboundMessage) |  |  |
| inboundMessage | [bytes](#bytes) |  |  |






<a name="signal.OutboundMessage"></a>

### OutboundMessage
Requires anonymous connection

A message from one client to another.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| to | [Address](#signal.Address) |  | The Address of the message recipient |
| unidentifiedSenderMessage | [bytes](#bytes) |  | A sealed sender message (opaque to the messaging-server). This is what will be delivered to the recipient. |






<a name="signal.PreKey"></a>

### PreKey
Information about a PreKey for a specific Address.

Clients will receive one of these for each device matching the query from RequestPreKeys.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| address | [Address](#signal.Address) |  | The Address that this key material belongs to |
| registrationID | [uint32](#uint32) |  | The local registrationID for the device at this Address. |
| signedPreKey | [bytes](#bytes) |  | The signedPreKey for the device at this Address. |
| oneTimePreKey | [bytes](#bytes) |  | One disposable preKey for the device at this Address. May be empty if none were available (that&#39;s okay, Signal can still do an X3DH key agreement without it). |






<a name="signal.PreKeysLow"></a>

### PreKeysLow
A notification from the server to the client that we&#39;re running low on oneTimePreKeys for the Address associated to this connection.

Clients may choose to respond to this by sending a Register message with some more preKeys. This does not have to be tied to the initial PreKeysLow message.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| keysRequested | [uint32](#uint32) |  | The number of additional oneTimePreKeys that the server is requesting. |






<a name="signal.Register"></a>

### Register
Requires authentication

A request to register a signed preKey and some set of one-time use preKeys. PreKeys are used by clients to perform X3DH key agreement in order to
establish end-to-end encrypted sessions.

This information is registered in the database under the client&#39;s Address. If multiple registrations are received, if the registrationID and signedPreKey
match the information on file, the new preKeys will be appended to the ones already on file. Otherwise, the existing registration will be replaced by the
latest.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| registrationID | [uint32](#uint32) |  | The local registrationID for this device. |
| signedPreKey | [bytes](#bytes) |  | The signedPreKey for this device. |
| oneTimePreKeys | [bytes](#bytes) | repeated | Zero, one or more disposable preKeys for this device. |






<a name="signal.RequestPreKeys"></a>

### RequestPreKeys
Requires anonymous connection

A request to retrieve preKey information for all registered devices for the given UserID except those listed in knownDeviceIDs.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| userID | [bytes](#bytes) |  | The UserID for which to retrieve preKeys. |
| knownDeviceIDs | [uint32](#uint32) | repeated | Devices which the client already knows about and doesn&#39;t need preKeys for. |






<a name="signal.Unregister"></a>

### Unregister
Requires authentication

Removes the recorded registration for the client&#39;s Address.





 

 

 

 



## Scalar Value Types

| .proto Type | Notes | C++ | Java | Python | Go | C# | PHP | Ruby |
| ----------- | ----- | --- | ---- | ------ | -- | -- | --- | ---- |
| <a name="double" /> double |  | double | double | float | float64 | double | float | Float |
| <a name="float" /> float |  | float | float | float | float32 | float | float | Float |
| <a name="int32" /> int32 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="int64" /> int64 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="uint32" /> uint32 | Uses variable-length encoding. | uint32 | int | int/long | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="uint64" /> uint64 | Uses variable-length encoding. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum or Fixnum (as required) |
| <a name="sint32" /> sint32 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sint64" /> sint64 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="fixed32" /> fixed32 | Always four bytes. More efficient than uint32 if values are often greater than 2^28. | uint32 | int | int | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="fixed64" /> fixed64 | Always eight bytes. More efficient than uint64 if values are often greater than 2^56. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum |
| <a name="sfixed32" /> sfixed32 | Always four bytes. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sfixed64" /> sfixed64 | Always eight bytes. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="bool" /> bool |  | bool | boolean | boolean | bool | bool | boolean | TrueClass/FalseClass |
| <a name="string" /> string | A string must always contain UTF-8 encoded or 7-bit ASCII text. | string | String | str/unicode | string | string | string | String (UTF-8) |
| <a name="bytes" /> bytes | May contain any arbitrary sequence of bytes. | string | ByteString | str | []byte | ByteString | string | String (ASCII-8BIT) |

