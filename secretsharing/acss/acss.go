package acss

import (
	"context"
	"crypto/sha256"
	"errors"
	"student_25_adkg/logging"
	"student_25_adkg/networking"
	"student_25_adkg/pedersencommitment"
	"student_25_adkg/rbc"
	"student_25_adkg/secretsharing"
	"student_25_adkg/typedefs"
	"sync"

	"github.com/rs/zerolog"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/encrypt/ecies"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/util/random"
	"google.golang.org/protobuf/proto"
)

// KeyStore allows getting the public key nodes
type KeyStore interface {
	// GetPublicKey returns the public key of the node with the given ID or an error
	// if no such key exist.
	GetPublicKey(id int64) (kyber.Point, error)
}

type ACSS struct {
	node                 *networking.Node
	config               secretsharing.Config
	rbc                  rbc.RBC[[]byte]
	ks                   KeyStore
	privateKey           kyber.Scalar
	instances            map[string]*Instance
	reconstructedChannel chan *Instance
	nodeIndex            int64
	logger               zerolog.Logger
	sync.RWMutex
}

func NewACSS(config secretsharing.Config, iface networking.NetworkInterface, rbc rbc.RBC[[]byte],
	ks KeyStore, privateKey kyber.Scalar, nodeIndex int64) *ACSS {
	return &ACSS{
		node:                 networking.NewNode(iface),
		config:               config,
		rbc:                  rbc,
		ks:                   ks,
		privateKey:           privateKey,
		instances:            make(map[string]*Instance),
		reconstructedChannel: make(chan *Instance, 100),
		nodeIndex:            nodeIndex,
		logger:               logging.GetLogger(nodeIndex),
	}
}

func (a *ACSS) Share(secret kyber.Scalar) (*Instance, error) {
	// Randomly sample a polynomial s.t. the origin is at s
	p := share.NewPriPoly(a.config.Group, a.config.Threshold, secret, random.New())
	commit, sShares, rShares, err := pedersencommitment.PedPolyCommit(p, a.config.Threshold,
		a.config.NbNodes, a.config.Group, a.config.Base0, a.config.Base1)
	if err != nil {
		return nil, err
	}

	encryptedShares, err := a.encryptShares(sShares, rShares)
	if err != nil {
		return nil, err
	}

	rbcPayloadMessage, err := a.createRBCPayload(commit, encryptedShares)
	if err != nil {
		return nil, err
	}

	rbcPayload, err := proto.Marshal(rbcPayloadMessage)

	rbcInstance, err := a.rbc.RBroadcast(rbcPayload)
	if err != nil {
		return nil, err
	}

	// Register this instance if it does not exist
	instance, created := a.getOrCreateInstance(rbcInstance.Identifier())
	if !created {
		return instance, errors.New("instance already exists with the same hash")
	}

	return instance, nil
}

func (a *ACSS) Reconstruct(instance *Instance) error {
	if !instance.RBCFinished() {
		return errors.New("instance is still in sharing phase")
	}

	sShare := instance.SShare()
	rShare := instance.RShare()

	if !a.verifyPedersenCommit(instance.Commit(), sShare, rShare) {
		return errors.New("commitment verification failed")
	}

	reconstructMessage, err := createReconstructMessage(sShare, rShare, instance.Identifier())
	if err != nil {
		return err
	}

	return a.broadcastACSSMessage(reconstructMessage)
}

func (a *ACSS) Start(ctx context.Context) error {
	// Set RBC to listen for messages
	go func() {
		_ = a.rbc.Listen(ctx)
	}()
	// Listen here for finished RBC instances
	go func() {
		a.listenToRBCFinishedChannel(ctx)
	}()
	// Start listening to the network
	a.node.SetCallback(a.handleMessage)
	return a.node.Start(ctx)
}

// GetInstances returns all the instances that this node has received
func (a *ACSS) GetInstances() []*Instance {
	a.RLock()
	defer a.RUnlock()
	instances := make([]*Instance, 0, len(a.instances))
	for _, i := range a.instances {
		instances = append(instances, i)
	}
	return instances
}

// GetIndex returns the index of this node in the protocol
func (a *ACSS) GetIndex() int64 {
	return a.nodeIndex
}

// predicate is the predicate function use in RBC used to verify
// the validity of the shares received.
func (a *ACSS) predicate(payload []byte) bool {
	rbcPayloadMessage := &typedefs.ACSSMessage_RBCPayload{}
	err := proto.Unmarshal(payload, rbcPayloadMessage)
	if err != nil {
		return false
	}

	commit, si, ri, err := a.parseRBCPayload(rbcPayloadMessage)
	if err != nil {
		return false
	}

	// Check the Pedersen commitment
	return a.verifyPedersenCommit(commit, si, ri)
}

// verifyPedersenCommit allows to more easily verify a commitment or shares without having to write
// the full method with the config that will be the same for every call
func (a *ACSS) verifyPedersenCommit(commit []kyber.Point, sShare, rShare *share.PriShare) bool {
	return pedersencommitment.PedPolyVerify(commit, int64(sShare.I), sShare, rShare, a.config.Group, a.config.Base0, a.config.Base1)
}

// encryptShares encrypt the given set of share.Prishare into bytes by marshalling each
// pair, concatenating them and encrypting using the node's private key.
// Returns an error if an error occurred while marshalling or encrypting
func (a *ACSS) encryptShares(sShares, rShares []*share.PriShare) ([][]byte, error) {
	// Encrypt the shares using the nodes public keys
	encryptedShares := make([][]byte, a.config.NbNodes)
	for i := 0; i < a.config.NbNodes; i++ {
		si := sShares[i]
		ri := rShares[i]
		pki, err := a.ks.GetPublicKey(int64(i + 1))
		if err != nil {
			return nil, err
		}

		// Encrypt si & ri
		siBytes, riBytes, err := secretsharing.MarshalShares(si, ri)
		toEncrypt := append(siBytes, riBytes...)

		encrypted, err := ecies.Encrypt(a.config.Group, pki, toEncrypt, sha256.New)
		encryptedShares[i] = encrypted
	}

	return encryptedShares, nil
}

// listenToRBCFinishedChannel listens for RBC instances to finish and handles them using handleFinishedRBC.
// Blocks until the context finishes.
func (a *ACSS) listenToRBCFinishedChannel(ctx context.Context) {
	run := true
	for run {
		select {
		case <-ctx.Done():
			run = false
		case state := <-a.rbc.GetFinishedChannel():
			a.handleFinishedRBC(state)
		}
	}
}

// handleFinishedRBC handles a finished RBC instance by notifying its corresponding
// ACSS instance that RBC finished (i.e. sharing finished). Creates the instance if
// it does not already exist.
func (a *ACSS) handleFinishedRBC(state rbc.Instance[[]byte]) {
	instance, _ := a.getOrCreateInstance(state.Identifier())
	if !state.Finished() || !state.Success() {
		return
	}

	payloadMessage := &typedefs.ACSSMessage_RBCPayload{}
	err := proto.Unmarshal(state.GetValue(), payloadMessage)
	if err != nil {
		return
	}

	commit, si, ri, err := a.parseRBCPayload(payloadMessage)
	if err != nil {
		return
	}

	instance.SetRBCResult(commit, si, ri)
}

// parseRBCPayload returns the commit and the shares marshalled into the RBC payload used by ACSS. Return an
// error if the payload can not be parsed.
func (a *ACSS) parseRBCPayload(payload *typedefs.ACSSMessage_RBCPayload) ([]kyber.Point, *share.PriShare, *share.PriShare, error) {
	commit, err := secretsharing.UnmarshalCommitment(payload.GetCommitment(), a.config.Group)

	encryptedShare := payload.GetEncryptedShares()[a.nodeIndex]

	plainShare, err := ecies.Decrypt(a.config.Group, a.privateKey, encryptedShare, sha256.New)
	if err != nil {
		return nil, nil, nil, err
	}

	siBytes := plainShare[:len(plainShare)/2]
	riBytes := plainShare[len(plainShare)/2:]

	si, ri, err := secretsharing.UnmarshalShares(siBytes, riBytes)
	if err != nil {
		return nil, nil, nil, err
	}

	return commit, si, ri, nil
}

// createRBCPayload embeds a Pedersen commitment and its encrypted shares into an RBC payload. Returns an error
// if marshalling failed.
func (a *ACSS) createRBCPayload(commitment []kyber.Point, encryptedShares [][]byte) (*typedefs.ACSSMessage_RBCPayload, error) {
	marshalledCommitment, err := secretsharing.MarshalCommitment(commitment)
	if err != nil {
		return nil, err
	}
	return &typedefs.ACSSMessage_RBCPayload{
		EncryptedShares: encryptedShares,
		Commitment:      marshalledCommitment,
	}, nil
}

// createReconstructMessage creates a RECONSTRUCT message with the given shares and instance identifier hash.
// Returns an error if marshalling failed.
func createReconstructMessage(si, ri *share.PriShare, identifier []byte) (*typedefs.ACSSMessage, error) {
	siBytes, riBytes, err := secretsharing.MarshalShares(si, ri)
	if err != nil {
		return nil, err
	}

	reconstructMessage := &typedefs.SSMessage_Reconstruct{
		Si: siBytes,
		Ri: riBytes,
	}

	reconstructMessageInstance := &typedefs.ACSSMessage_ReconstructInst{ReconstructInst: reconstructMessage}

	acssMessage := &typedefs.ACSSMessage{
		Op:                 reconstructMessageInstance,
		InstanceIdentifier: identifier,
	}

	return acssMessage, nil
}

// broadcastACSSMessage broadcast typedefs.ACSSMessage as a typedefs.Packet via the node.
// Returns an error if the broadcast failed.
func (a *ACSS) broadcastACSSMessage(acssMessage *typedefs.ACSSMessage) error {
	message := &typedefs.Packet_AcssMessage{AcssMessage: acssMessage}
	packet := &typedefs.Packet{Message: message}
	return a.node.Broadcast(packet)
}

// getInstance returns Instance identified by the given hash. The second return value
// indicates whether the Instance was found (true) or not (false).
func (a *ACSS) getInstance(hash []byte) (*Instance, bool) {
	a.RLock()
	defer a.RUnlock()
	instance, ok := a.instances[string(hash)]
	return instance, ok
}

// getOrCreateInstance returns the Instance identified by the given has or creates it if it
// does not exist. Second return value indicates whether the Instance has just been created (true)
// or not (false). In both cases, the Instance returned will be valid.
func (a *ACSS) getOrCreateInstance(hash []byte) (*Instance, bool) {
	a.Lock()
	defer a.Unlock()
	if instance, ok := a.instances[string(hash)]; ok {
		return instance, false
	}
	instance := NewInstance(hash)
	a.instances[string(hash)] = instance
	return instance, true
}

// handleMessage handles a typedefs.Packet received on the network. This method is used as callback
// for the networking.Node used in this ACSS node.
func (a *ACSS) handleMessage(message *typedefs.Packet) error {
	acssMessage, ok := message.GetMessage().(*typedefs.Packet_AcssMessage)
	if !ok {
		return errors.New("message is not of type *typedefs.ACSSMessage")
	}
	var err error

	instance, ok := a.getInstance(acssMessage.AcssMessage.InstanceIdentifier)
	if !ok {
		return errors.New("instance not found")
	}

	switch m := acssMessage.AcssMessage.GetOp().(type) {
	case *typedefs.ACSSMessage_ImplicateInst:
		return errors.New("not implemented")
	case *typedefs.ACSSMessage_RecoverInst:
		return errors.New("not implemented")
	case *typedefs.ACSSMessage_ReconstructInst:
		err = a.receiveReconstructMessage(m.ReconstructInst, instance)
	}

	return err
}

// receiveReconstructMessage handles the logic when a RECONSTRUCT message is received.
// Returns an error if unmarshalling caused an error.
func (a *ACSS) receiveReconstructMessage(message *typedefs.SSMessage_Reconstruct, instance *Instance) error {
	sj, rj, err := secretsharing.UnmarshalShares(message.GetSi(), message.GetRi())
	if err != nil {
		return err
	}

	if !a.verifyPedersenCommit(instance.Commit(), sj, rj) {
		return errors.New("commitment verification failed")
	}

	nbShares := instance.AppendSShare(sj)
	if nbShares < a.config.Threshold+1 {
		return nil
	}

	scalar, err := a.reconstruct(instance.SShares())
	if err != nil {
		return err
	}

	instance.SetReconstructed(scalar)
	a.reconstructedChannel <- instance
	return nil
}

// reconstruct tries to reconstruct the secret from the given set of shares by interpolating
// a polynomial of degree t where t is the threshold in the config
func (a *ACSS) reconstruct(shares []*share.PriShare) (kyber.Scalar, error) {
	// Linear interpolation to get the polynomial
	poly, err := share.RecoverPriPoly(a.config.Group, shares, a.config.Threshold, a.config.NbNodes)
	if err != nil {
		return nil, err
	}
	return poly.Secret(), nil
}
