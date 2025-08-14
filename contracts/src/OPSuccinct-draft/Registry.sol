// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.30;

contract Registry {
    struct RollupInfo {
        uint256 chainId;
        bytes32 genesisHash;
        bytes32 sequencerPubKey;
        string endpoint;
        uint256 startingSlot;
        bool active;
    }

    mapping(uint256 => RollupInfo) public rollups;
    uint256[] public activeChainIds;
    mapping(uint256 => uint256) private chainIdToIndex;

    event RollupRegistered(uint256 chainId, bytes32 genesisHash, bytes32 sequencerPubKey, string endpoint, uint256 startingSlot);
    event RollupRemoved(uint256 chainId);

    function registerRollup(
        uint256 _chainId,
        bytes32 _genesisHash,
        bytes32 _sequencerPubKey,
        string calldata _endpoint,
        uint256 _startingSlot
    ) external {
        require(_chainId != 0, "invalid chain ID");
        require(!rollups[_chainId].active, "already registered");

        rollups[_chainId] = RollupInfo({
            chainId: _chainId,
            genesisHash: _genesisHash,
            sequencerPubKey: _sequencerPubKey,
            endpoint: _endpoint,
            startingSlot: _startingSlot,
            active: true
        });

        activeChainIds.push(_chainId);
        chainIdToIndex[_chainId] = activeChainIds.length - 1;

        emit RollupRegistered(_chainId, _genesisHash, _sequencerPubKey, _endpoint, _startingSlot);
    }

    function removeRollup(uint256 _chainId) external {
        require(rollups[_chainId].active, "not registered");

        rollups[_chainId].active = false;

        uint256 index = chainIdToIndex[_chainId];
        uint256 lastId = activeChainIds[activeChainIds.length - 1];

        activeChainIds[index] = lastId;
        chainIdToIndex[lastId] = index;
        activeChainIds.pop();
        delete chainIdToIndex[_chainId];

        emit RollupRemoved(_chainId);
    }

    function getActiveChainIds() external view returns (uint256[] memory) {
        return activeChainIds;
    }

    function getRollupInfo(uint256 _chainId) external view returns (RollupInfo memory) {
        return rollups[_chainId];
    }
}
