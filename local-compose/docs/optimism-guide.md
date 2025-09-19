(source from https://docs.optimism.io/operators/chain-operators/tutorials/create-l2-rollup)

Creating your own L2 rollup testnet
Please be prepared to set aside approximately one hour to get everything running properly and make sure to read through the guide carefully. You don't want to miss any important steps that might cause issues down the line.

This tutorial is designed for developers who want to learn about the OP Stack by spinning up an OP Stack testnet chain. You'll walk through the full deployment process and teach you all of the components that make up the OP Stack, and you'll end up with your very own OP Stack testnet.

It's useful to understand what each of these components does before you start deploying your chain. To learn about the different components please read the deployment overview page.

You can use this testnet to experiment and perform tests, or you can choose to modify the chain to adapt it to your own needs. The OP Stack is free and open source software licensed entirely under the MIT license. You don't need permission from anyone to modify or deploy the stack in any configuration you want.

Modifications to the OP Stack may prevent a chain from being able to benefit from aspects of the Optimism Superchain. Make sure to check out the Superchain Explainer to learn more.

Software dependencies
Dependency	Version	Version Check Command
git	^2	git --version
go	^1.21	go version
node	^20	node --version
pnpm	^8	pnpm --version
foundry	^0.2.0	forge --version
make	^3	make --version
jq	^1.6	jq --version
direnv	^2	direnv --version
Notes on specific dependencies
node
We recommend using the latest LTS version of Node.js (currently v20). nvm is a useful tool that can help you manage multiple versions of Node.js on your machine. You may experience unexpected errors on older versions of Node.js.

foundry
It's recommended to use the scripts in the monorepo's package.json for managing foundry to ensure you're always working with the correct version. This approach simplifies the installation, update, and version checking process. Make sure to clone the monorepo locally before proceeding.

direnv
Parts of this tutorial use direnv as a way of loading environment variables from .envrc files into your shell. This means you won't have to manually export environment variables every time you want to use them. direnv only ever has access to files that you explicitly allow it to see.

After installing direnv, you will need to make sure that direnv is hooked into your shell. Make sure you've followed the guide on the direnv website, then close your terminal and reopen it so that the changes take effect (or source your config file if you know how to do that).

Make sure that you have correctly hooked direnv into your shell by modifying your shell configuration file (like ~/.bashrc or ~/.zshrc). If you haven't edited a config file then you probably haven't configured direnv properly (and things might not work later).

Get access to a sepolia node
You'll be deploying a OP Stack Rollup chain that uses a Layer 1 blockchain to host and order transaction data. The OP Stack Rollups were designed to use EVM Equivalent blockchains like Ethereum, OP Mainnet, or standard Ethereum testnets as their L1 chains.

This guide uses the Sepolia testnet as an L1 chain. We recommend that you also use Sepolia. You can also use other EVM-compatible blockchains, but you may run into unexpected errors. If you want to use an alternative network, make sure to carefully review each command and replace any Sepolia-specific values with the values for your network.

Since you're deploying your OP Stack chain to Sepolia, you'll need to have access to a Sepolia node. You can either use a node provider like Alchemy (easier) or run your own Sepolia node (harder).

Build the source code
You're going to be spinning up your OP Stack chain directly from source code instead of using a container system like Docker. Although this adds a few extra steps, it means you'll have an easier time modifying the behavior of the stack if you'd like to do so. If you want a summary of the various components you'll be using, take another look at the What You're Going to Deploy section above.

You're using the home directory ~/ as the work directory for this tutorial for simplicity. You can use any directory you'd like but using the home directory will allow you to copy/paste the commands in this guide. If you choose to use a different directory, make sure you're using the correct directory in the commands throughout this tutorial.

Build the Optimism monorepo
Clone the Optimism Monorepo
cd ~
git clone https://github.com/ethereum-optimism/optimism.git

Enter the Optimism Monorepo
cd optimism

Check out the correct branch
You will be using the tutorials/chain branch of the Optimism Monorepo to deploy an OP Stack testnet chain during this tutorial. This is a non-production branch that lags behind the develop branch. You should NEVER use the develop or tutorials/chain branches in production.

git checkout tutorials/chain

Check your dependencies
Don't skip this step! Make sure you have all of the required dependencies installed before continuing.

Run the following script and double check that you have all of the required versions installed. If you don't have the correct versions installed, you may run into unexpected errors.

./packages/contracts-bedrock/scripts/getting-started/versions.sh

Install dependencies
pnpm install

Build the various packages inside of the Optimism Monorepo
make op-node op-batcher op-proposer
pnpm build

Build op-geth
Clone op-geth
cd ~
git clone https://github.com/ethereum-optimism/op-geth.git

Enter op-geth
cd op-geth

Build op-geth
make geth

Fill out environment variables
You'll need to fill out a few environment variables before you can start deploying your chain.

Enter the Optimism Monorepo
cd ~/optimism

Duplicate the sample environment variable file
cp .envrc.example .envrc

Fill out the environment variable file
Open up the environment variable file and fill out the following variables:

Variable Name	Description
L1_RPC_URL	URL for your L1 node (a Sepolia node in this case).
L1_RPC_KIND	Kind of L1 RPC you're connecting to, used to inform optimal transactions receipts fetching. Valid options: alchemy, quicknode, infura, parity, nethermind, debug_geth, erigon, basic, any.
Generate addresses
You'll need four addresses and their private keys when setting up the chain:

The Admin address has the ability to upgrade contracts.
The Batcher address publishes Sequencer transaction data to L1.
The Proposer address publishes L2 transaction results (state roots) to L1.
The Sequencer address signs blocks on the p2p network.
Enter the Optimism Monorepo
cd ~/optimism

Generate new addresses
You should not use the wallets.sh tool for production deployments. If you are deploying an OP Stack based chain into production, you should likely be using a combination of hardware security modules and hardware wallets.

./packages/contracts-bedrock/scripts/getting-started/wallets.sh

Check the output
Make sure that you see output that looks something like the following:

Copy the following into your .envrc file:
  
# Admin address
export GS_ADMIN_ADDRESS=0x9625B9aF7C42b4Ab7f2C437dbc4ee749d52E19FC
export GS_ADMIN_PRIVATE_KEY=0xbb93a75f64c57c6f464fd259ea37c2d4694110df57b2e293db8226a502b30a34
# Batcher address
export GS_BATCHER_ADDRESS=0xa1AEF4C07AB21E39c37F05466b872094edcf9cB1
export GS_BATCHER_PRIVATE_KEY=0xe4d9cd91a3e53853b7ea0dad275efdb5173666720b1100866fb2d89757ca9c5a
  
# Proposer address
export GS_PROPOSER_ADDRESS=0x40E805e252D0Ee3D587b68736544dEfB419F351b
export GS_PROPOSER_PRIVATE_KEY=0x2d1f265683ebe37d960c67df03a378f79a7859038c6d634a61e40776d561f8a2
  
# Sequencer address
export GS_SEQUENCER_ADDRESS=0xC06566E8Ec6cF81B4B26376880dB620d83d50Dfb
export GS_SEQUENCER_PRIVATE_KEY=0x2a0290473f3838dbd083a5e17783e3cc33c905539c0121f9c76614dda8a38dca

Save the addresses
Copy the output from the previous step and paste it into your .envrc file as directed.

Fund the addresses
You will need to send ETH to the Admin, Proposer, and Batcher addresses. The exact amount of ETH required depends on the L1 network being used. You do not need to send any ETH to the Sequencer address as it does not send transactions.

It's recommended to fund the addresses with the following amounts when using Sepolia:

Admin — 0.5 Sepolia ETH
Batcher — 0.1 Sepolia ETH
Proposer — 0.2 Sepolia ETH
To get the required Sepolia ETH to fund the addresses, we recommend using the Superchain Faucet together with Coinbase verification.

Load environment variables
Now that you've filled out the environment variable file, you need to load those variables into your terminal.

Enter the Optimism Monorepo
cd ~/optimism

Load the variables with direnv
You're about to use direnv to load environment variables from the .envrc file into your terminal. Make sure that you've installed direnv and that you've properly hooked direnv into your shell.

Next you'll need to allow direnv to read this file and load the variables into your terminal using the following command.

direnv allow

WARNING: direnv will unload itself whenever your .envrc file changes. You must rerun the following command every time you change the .envrc file.

Confirm that the variables were loaded
After running direnv allow you should see output that looks something like the following (the exact output will vary depending on the variables you've set, don't worry if it doesn't look exactly like this):

direnv: loading ~/optimism/.envrc                                                            
direnv: export +DEPLOYMENT_CONTEXT +ETHERSCAN_API_KEY +GS_ADMIN_ADDRESS +GS_ADMIN_PRIVATE_KEY +GS_BATCHER_ADDRESS +GS_BATCHER_PRIVATE_KEY +GS_PROPOSER_ADDRESS +GS_PROPOSER_PRIVATE_KEY +GS_SEQUENCER_ADDRESS +GS_SEQUENCER_PRIVATE_KEY +IMPL_SALT +L1_RPC_KIND +L1_RPC_URL +PRIVATE_KEY +TENDERLY_PROJECT +TENDERLY_USERNAME

If you don't see this output, you likely haven't properly configured direnv. Make sure you've configured direnv properly and run direnv allow again so that you see the desired output.

Configure your network
Once you've built both repositories, you'll need to head back to the Optimism Monorepo to set up the configuration file for your chain. Currently, chain configuration lives inside of the contracts-bedrock package in the form of a JSON file.

Enter the Optimism Monorepo
cd ~/optimism

Move into the contracts-bedrock package
cd packages/contracts-bedrock

Install Foundry dependencies
forge install

Generate the configuration file
Run the following script to generate the getting-started.json configuration file inside of the deploy-config directory.

./scripts/getting-started/config.sh

Review the configuration file (Optional)
If you'd like, you can review the configuration file that was just generated by opening up deploy-config/getting-started.json in your favorite text editor. It's recommended to keep this file as-is for now so you don't run into any unexpected errors.

Deploy the Create2 factory (optional)
If you're deploying an OP Stack chain to a network other than Sepolia, you may need to deploy a Create2 factory contract to the L1 chain. This factory contract is used to deploy OP Stack smart contracts in a deterministic fashion.

This step is typically only necessary if you are deploying your OP Stack chain to custom L1 chain. If you are deploying your OP Stack chain to Sepolia, you can safely skip this step.

Check if the factory exists
The Create2 factory contract will be deployed at the address 0x4e59b44847b379578588920cA78FbF26c0B4956C. You can check if this contract has already been deployed to your L1 network with a block explorer or by running the following command:

cast codesize 0x4e59b44847b379578588920cA78FbF26c0B4956C --rpc-url $L1_RPC_URL

If the command returns 0 then the contract has not been deployed yet. If the command returns 69 then the contract has been deployed and you can safely skip this section.

Fund the factory deployer
You will need to send some ETH to the address that will be used to deploy the factory contract, 0x3fAB184622Dc19b6109349B94811493BF2a45362. This address can only be used to deploy the factory contract and will not be used for anything else. Send at least 1 ETH to this address on your L1 chain.

Deploy the factory
Using cast, deploy the factory contract to your L1 chain:

cast publish --rpc-url $L1_RPC_URL 0xf8a58085174876e800830186a08080b853604580600e600039806000f350fe7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe03601600081602082378035828234f58015156039578182fd5b8082525050506014600cf31ba02222222222222222222222222222222222222222222222222222222222222222a02222222222222222222222222222222222222222222222222222222222222222 

Wait for the transaction to be mined
Make sure that the transaction is included in a block on your L1 chain before continuing.

Verify that the factory was deployed
Run the code size check again to make sure that the factory was properly deployed:

cast codesize 0x4e59b44847b379578588920cA78FbF26c0B4956C --rpc-url $L1_RPC_URL

Deploy the L1 contracts
Once you've configured your network, it's time to deploy the L1 contracts necessary for the functionality of the chain.

Using op-deployer
The op-deployer tool simplifies the creation of genesis and rollup configuration files (genesis.json and rollup.json). These files are crucial for initializing the execution client (op-geth) and consensus client (op-node) for your network.

The recommended flow for creating a genesis file and rollup configuration file on the OP Stack is as follows:

Deploy the L1 contracts using op-deployer.
Generate both the L2 genesis file (genesis.json) and the rollup configuration file (rollup.json) using op-deployer's inspect commands.
Initialize your off-chain components (e.g., execution client, consensus client).
Using op-deployer for chain initialization is a requirement for all chains intending to be for chains who intend to be standard and join the superchain. This ensures standardization and compatibility across the OP Stack ecosystem.

Prerequisites
You have installed the op-deployer binary following the instructions in deployer docs. After installation, extract the op-deployer into your PATH and cd op-deployer.

You have created and customized an intent file in a .deployer directory, typically by running:

./bin/op-deployer init --l1-chain-id <YOUR_L1_CHAIN_ID> --l2-chain-ids <YOUR_L2_CHAIN_ID> --workdir .deployer

Replace <YOUR_L1_CHAIN_ID> and <YOUR_L2_CHAIN_ID> with their respective values, see a list of chainIds.

You have edited that intent file to your liking (roles, addresses, etc.).

Step 1: Deploy the L1 contracts
To deploy your chain to L1, run:

./bin/op-deployer apply --workdir .deployer \
  --l1-rpc-url <RPC_URL_FOR_L1> \
  --private-key <DEPLOYER_PRIVATE_KEY_HEX>

Replace <RPC_URL_FOR_L1> with the L1 RPC URL.
Replace <DEPLOYER_PRIVATE_KEY_HEX> with the private key of the account used for deployment.
This command:

Reads your intent file in .deployer/.
Deploys the OP Stack contracts to the specified L1.
Updates a local state.json file with the results of the deployment.
Step 2: Generate your L2 genesis file and rollup file
After your L1 contracts have been deployed, generate the L2 genesis and rollup configuration files by inspecting the deployer's state.json.

./bin/op-deployer inspect genesis --workdir .deployer <L2_CHAIN_ID> > .deployer/genesis.json
./bin/op-deployer inspect rollup --workdir .deployer <L2_CHAIN_ID> > .deployer/rollup.json

genesis.json is the file you will provide to your execution client (e.g. op-geth).
rollup.json is the file you will provide to your consensus client (e.g. op-node).
Step 3: Initialize your off-chain components
Once you have genesis.json and rollup.json:

Initialize op-geth using genesis.json.
Configure op-node with rollup.json.
Set up additional off-chain infrastructure as needed (block explorer, indexers, etc.). For more on architecture, see Architecture overview.
Initialize op-geth
You're almost ready to run your chain! Now you just need to run a few commands to initialize op-geth. You're going to be running a Sequencer node, so you'll need to import the Sequencer private key that you generated earlier. This private key is what your Sequencer will use to sign new blocks.

Navigate to the op-geth directory
cd ~/op-geth

Create a data directory folder
mkdir datadir

Build the op-geth binary
make geth

Initialize op-geth
build/bin/geth init --state.scheme=hash --datadir=datadir genesis.json

Start op-geth
Now you'll start op-geth, your Execution Client. Note that you won't start seeing any transactions until you start the Consensus Client in the next step.

Open up a new terminal
You'll need a terminal window to run op-geth in.

Navigate to the op-geth directory
cd ~/op-geth

Run op-geth
You're using --gcmode=archive to run op-geth here because this node will act as your Sequencer. It's useful to run the Sequencer in archive mode because the op-proposer requires access to the full state. Feel free to run other (non-Sequencer) nodes in full mode if you'd like to save disk space. Just make sure at least one other archive node exists and the op-proposer points to it.

It's important that you've already initialized the geth node at this point as per the previous section. Failure to do this will cause startup issues between op-geth and op-node.

./build/bin/geth \
  --datadir ./datadir \
  --http \
  --http.corsdomain="*" \
  --http.vhosts="*" \
  --http.addr=0.0.0.0 \
  --http.api=web3,debug,eth,txpool,net,engine \
  --ws \
  --ws.addr=0.0.0.0 \
  --ws.port=8546 \
  --ws.origins="*" \
  --ws.api=debug,eth,txpool,net,engine \
  --syncmode=full \
  --gcmode=archive \
  --nodiscover \
  --maxpeers=0 \
  --networkid=42069 \
  --authrpc.vhosts="*" \
  --authrpc.addr=0.0.0.0 \
  --authrpc.port=8551 \
  --authrpc.jwtsecret=./jwt.txt \
  --rollup.disabletxpoolgossip=true

Start op-node
Once you've got op-geth running you'll need to run op-node. Like Ethereum, the OP Stack has a Consensus Client (op-node) and an Execution Client (op-geth). The Consensus Client "drives" the Execution Client over the Engine API.

Open up a new terminal
You'll need a terminal window to run the op-node in.

Navigate to the op-node directory
cd ~/optimism/op-node

Run op-node
./bin/op-node \
  --l2=http://localhost:8551 \
  --l2.jwt-secret=./jwt.txt \
  --sequencer.enabled \
  --sequencer.l1-confs=5 \
  --verifier.l1-confs=4 \
  --rollup.config=./rollup.json \
  --rpc.addr=0.0.0.0 \
  --p2p.disable \
  --rpc.enable-admin \
  --p2p.sequencer.key=$GS_SEQUENCER_PRIVATE_KEY \
  --l1=$L1_RPC_URL \
  --l1.rpckind=$L1_RPC_KIND

Once you run this command, you should start seeing the op-node begin to sync L2 blocks from the L1 chain. Once the op-node has caught up to the tip of the L1 chain, it'll begin to send blocks to op-geth for execution. At that point, you'll start to see blocks being created inside of op-geth.

By default, your op-node will try to use a peer-to-peer to speed up the synchronization process. If you're using a chain ID that is also being used by others, like the default chain ID for this tutorial (42069), your op-node will receive blocks signed by other sequencers. These requests will fail and waste time and network resources. To avoid this, this tutorial starts with peer-to-peer synchronization disabled (--p2p.disable).

Once you have multiple nodes, you may want to enable peer-to-peer synchronization. You can add the following options to the op-node command to enable peer-to-peer synchronization with specific nodes:

  --p2p.static=<nodes> \
  --p2p.listen.ip=0.0.0.0 \
  --p2p.listen.tcp=9003 \
  --p2p.listen.udp=9003 \

You can alternatively also remove the --p2p.static option, but you may see failed requests from other chains using the same chain ID.

Start op-batcher
The op-batcher takes transactions from the Sequencer and publishes those transactions to L1. Once these Sequencer transactions are included in a finalized L1 block, they're officially part of the canonical chain. The op-batcher is critical!

It's best to give the Batcher address at least 1 Sepolia ETH to ensure that it can continue operating without running out of ETH for gas. Keep an eye on the balance of the Batcher address because it can expend ETH quickly if there are a lot of transactions to publish.

Open up a new terminal
You'll need a terminal window to run the op-batcher in.

Navigate to the op-batcher directory
cd ~/optimism/op-batcher

Run op-batcher
./bin/op-batcher \
  --l2-eth-rpc=http://localhost:8545 \
  --rollup-rpc=http://localhost:9545 \
  --poll-interval=1s \
  --sub-safety-margin=6 \
  --num-confirmations=1 \
  --safe-abort-nonce-too-low-count=3 \
  --resubmission-timeout=30s \
  --rpc.addr=0.0.0.0 \
  --rpc.port=8548 \
  --rpc.enable-admin \
  --max-channel-duration=25 \
  --l1-eth-rpc=$L1_RPC_URL \
  --private-key=$GS_BATCHER_PRIVATE_KEY

The --max-channel-duration=n setting tells the batcher to write all the data to L1 every n L1 blocks. When it is low, transactions are written to L1 frequently and other nodes can synchronize from L1 quickly. When it is high, transactions are written to L1 less frequently and the batcher spends less ETH. If you want to reduce costs, either set this value to 0 to disable it or increase it to a higher value.

Start op-proposer
Now start op-proposer, which proposes new state roots.

Open up a new terminal
You'll need a terminal window to run the op-proposer in.

Navigate to the op-proposer directory
cd ~/optimism/op-proposer

Run op-proposer
./bin/op-proposer \
  --poll-interval=12s \
  --rpc.port=8560 \
  --rollup-rpc=http://localhost:9545 \
  --l2oo-address=$(cat ../packages/contracts-bedrock/deployments/getting-started/.deploy | jq -r .L2OutputOracleProxy) \
  --private-key=$GS_PROPOSER_PRIVATE_KEY \
  --l1-eth-rpc=$L1_RPC_URL

Connect your wallet to your chain
You now have a fully functioning OP Stack Rollup with a Sequencer node running on http://localhost:8545. You can connect your wallet to this chain the same way you'd connect your wallet to any other EVM chain. If you need an easy way to connect to your chain, just click here.

Get ETH on your chain
Once you've connected your wallet, you'll probably notice that you don't have any ETH to pay for gas on your chain. The easiest way to deposit Sepolia ETH into your chain is to send ETH directly to the L1StandardBridge contract.

Navigate to the contracts-bedrock directory
cd ~/optimism/packages/contracts-bedrock

Get the address of the L1StandardBridgeProxy contract
cat deployments/getting-started/.deploy | jq -r .L1StandardBridgeProxy

Send some Sepolia ETH to the L1StandardBridgeProxy contract
Grab the L1 bridge proxy contract address and, using the wallet that you want to have ETH on your Rollup, send that address a small amount of ETH on Sepolia (0.1 or less is fine). This will trigger a deposit that will mint ETH into your wallet on L2. It may take up to 5 minutes for that ETH to appear in your wallet on L2.

See your rollup in action
You can interact with your Rollup the same way you'd interact with any other EVM chain. Send some transactions, deploy some contracts, and see what happens!

Next steps
Check out the protocol specs for more detail about the rollup protocol.
If you run into any problems, please visit the Chain Operators Troubleshooting Guide for help.
