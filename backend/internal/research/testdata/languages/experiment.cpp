#include <nlohmann/json.hpp>
#include <iostream>
int main(){using nlohmann::json;json req;std::cin>>req;double sum=req["inputs"]["a"].get<double>()+std::stod(req["dataset_rows"][0]["value"].get<std::string>());json result={{"kind","computational_experiment"},{"metrics",{{"sum",sum}}},{"findings",json::array({"Computed sum"})},{"limitations",json::array({"Test computation only"})}};std::cout<<result.dump();}
