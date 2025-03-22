// SPDX-License-Identifier: MIT
pragma solidity ^0.8.12;

contract InfTESLAplusplus {
    address cm = msg.sender;

    struct rcd {
        bool isTaken;
        address owner;
        mapping(uint => uint) startTime;
        mapping(uint => uint) endTime;
        mapping(uint => uint) disclosureDelay;
        mapping(uint => string) key;
        mapping(uint => bool) isIndexExist;
        uint currentIndex;
    }

    mapping(uint => rcd) RCDs;

    modifier onlyCM() {
        require(msg.sender == cm, "only CM can call");
        _;
    }

    function regOwner(address _owner, uint _rcd) public onlyCM {
        require(RCDs[_rcd].isTaken == false, "rcd id is already in use");
        RCDs[_rcd].owner = _owner;
        RCDs[_rcd].isTaken = true;
        RCDs[_rcd].currentIndex = 0;
    }

    function storeKey(
        uint _rcd,
        uint _index,
        string memory _key,
        uint _startTime,
        uint _endTime,
        uint _disclosurDelay
    ) public {
        require(
            RCDs[_rcd].isTaken == true,
            "Keys can be stored for only active RCDs"
        );
        require(
            RCDs[_rcd].owner == msg.sender,
            "Only owner of the RCD can call"
        );
        require(
            _index > RCDs[_rcd].currentIndex,
            "Only future keys can be stored"
        );
        require(
            RCDs[_rcd].currentIndex == 0 ||
                RCDs[_rcd].endTime[_index - 1] < _startTime,
            "New keychain can only be started after the old keychain ends"
        );
        RCDs[_rcd].key[_index] = _key;
        RCDs[_rcd].startTime[_index] = _startTime;
        RCDs[_rcd].endTime[_index] = _endTime;
        RCDs[_rcd].disclosureDelay[_index] = _disclosurDelay;
        RCDs[_rcd].isIndexExist[_index] = true;
        if (RCDs[_rcd].currentIndex == 0) {
            RCDs[_rcd].currentIndex = 1;
        }
    }

    function getKey(
        uint _rcd
    ) public view returns (string memory, uint, uint, uint) {
        uint currentIndex = RCDs[_rcd].currentIndex;
        return (
            RCDs[_rcd].key[currentIndex],
            RCDs[_rcd].startTime[currentIndex],
            RCDs[_rcd].endTime[currentIndex],
            RCDs[_rcd].disclosureDelay[currentIndex]
        );
    }

    function changeCurrentIndex(uint _rcd) public {
        uint currentIndex = RCDs[_rcd].currentIndex;
        require(
            RCDs[_rcd].endTime[currentIndex] < block.number,
            "New chain can only started after the old chain expires"
        );
        require(
            RCDs[_rcd].isIndexExist[currentIndex + 1] == true,
            "only stored index can be incremented "
        );
        RCDs[_rcd].currentIndex++;
    }
}
