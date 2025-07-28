# :construction_worker: :closed_lock_with_key: __Rollup Contracts__

:construction: CAUTION: This repo is currently under **heavy development!** :construction:

## :page_with_curl: _Instructions_

**1)** Fire up your favorite console & clone this repo somewhere:

__`❍ git clone https://github.com/ssvlabs/rollup-shared-publisher.git`__

**2)** After selecting the right branch, enter this directory & install dependencies:

__`❍ cd rollup-shared-publisher/contracts && forge install`__

**3)** Compile the contracts:

__`❍ forge build`__

**4)** Set the tests going!

__`❍ forge test`__

**5)** Set your env adding your private key and public address!

__`❍ cp .env.example .env`__

**6)** Deploy on Rollup Hoodi:

__`❍ npm run deploy:rollup-hoodi`__

The coordinator is going to be the deployer address;

**7)** Deploy on Rollup Sepolia:

__`❍ npm run deploy:rollup-sepolia`__

The coordinator is going to be the deployer address.


## Deploy To Rollup

its bridge address proxy
in metamask (or via cast call) transfer ETH to 0x186f7cb956bccf5b6bfef00427e9f2121275bfd4
connect metamask to OP stage rollup RPC: http://57.129.73.156:31130/
with chain id 11111